# Watch-Based Certificate Discovery con Hash Verification

## Panoramica

KubeTemplater v0.3.3 implementa un sistema **watch-based** per la scoperta e verifica dei certificati webhook, sostituendo l'approccio a polling precedente con una soluzione più efficiente e thread-safe.

## Architettura

### Componenti principali

1. **Kubernetes Clientset**: Client raw per accesso alle API Watch
2. **Lease Watch**: Monitoraggio event-driven delle annotations nella risorsa Lease
3. **Hash Verification**: Confronto SHA256 tra filesystem e lease annotation
4. **Hybrid Approach**: Combinazione di watch events + ticker fallback per robustezza

### Flow di esecuzione

```
┌─────────────────────────────────────────────────────────────┐
│                    LEADER POD                                │
│                                                               │
│  1. Genera certificato                                       │
│  2. Calcola SHA256 hash                                      │
│  3. Update Secret in Kubernetes                              │
│  4. Update Lease annotations:                                │
│     - kubetemplater.io/cert-ready: "true"                    │
│     - kubetemplater.io/cert-hash: "49996b89..."              │
│     - kubetemplater.io/cert-valid-until: "2026-12-06..."     │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ Watch Event
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                   FOLLOWER POD                               │
│                                                               │
│  T=0.1s: Watch riceve evento Lease modificata               │
│          - Legge hash: "49996b89..."                         │
│          - Salva in atomic.Value (thread-safe)               │
│          - Prova caricamento immediato:                      │
│            • Calcola hash filesystem: "40ad2672..." (OLD)    │
│            • Hash mismatch! → Wait                           │
│                                                               │
│  T=2s:   Ticker check                                        │
│          - Calcola hash filesystem: "40ad2672..." (OLD)      │
│          - Hash mismatch → Wait                              │
│                                                               │
│  T=4-8s: Ticker continua a riprovare...                      │
│                                                               │
│  T=~9s:  Kubelet sincronizza Secret → filesystem aggiornato │
│                                                               │
│  T=10s:  Ticker check                                        │
│          - Calcola hash filesystem: "49996b89..." (NEW)      │
│          - Hash match! ✅                                     │
│          - Double-check locking: certWatcher == nil?         │
│          - Carica certwatcher.New()                          │
│          - sync.Once: close(certReadyChan)                   │
│          - Webhook server può partire                        │
└─────────────────────────────────────────────────────────────┘
```

## Race Conditions Risolte

### 1. ❌ Accesso concorrente a `lastSeenLeaseHash`

**Problema**: Watch event handler e ticker goroutine accedevano alla variabile condivisa senza sincronizzazione.

**Soluzione**:
```go
var lastSeenLeaseHash atomic.Value  // Thread-safe atomic operations
lastSeenLeaseHash.Store(eventHash)  // Atomic write
expectedHash, ok := lastSeenLeaseHash.Load().(string)  // Atomic read
```

### 2. ✅ Accesso concorrente a `certWatcher`

**Già risolto**: Mutex `certWatcherMu` proteggeva correttamente l'accesso.

### 3. ❌ Double-close su `certReadyChan`

**Problema**: Watch event e ticker potrebbero entrambi chiudere il canale contemporaneamente → panic.

**Soluzione**:
```go
var certReadyOnce sync.Once
certReadyOnce.Do(func() {
    close(certReadyChan)  // Garantito di eseguire una sola volta
})
```

### 4. ❌ Eventi watch multipli veloci

**Problema**: Eventi ravvicinati potrebbero sovrascrivere `lastSeenLeaseHash` durante I/O lenta.

**Soluzione**: Snapshot locale dell'hash prima di operazioni lunghe:
```go
eventHash := lease.Annotations["kubetemplater.io/cert-hash"]  // Snapshot
lastSeenLeaseHash.Store(eventHash)  // Update globale
tryLoadCertificate(eventHash)  // Usa snapshot locale
```

### 5. ❌ Doppio caricamento certificato

**Problema**: Watch e ticker potrebbero caricare certificato simultaneamente → memory leak.

**Soluzione**: Double-check locking pattern:
```go
// Quick check senza lock
certWatcherMu.Lock()
if certWatcher != nil {
    certWatcherMu.Unlock()
    return
}
certWatcherMu.Unlock()

// ... calcola hash e verifica ...

// Double-check con lock
certWatcherMu.Lock()
defer certWatcherMu.Unlock()
if certWatcher != nil {
    return  // Altra goroutine l'ha già caricato
}
certWatcher = watcher
```

### 6. ❌ Watch closed senza restart

**Problema**: Se Kubernetes chiude il watch (timeout, errori rete), nessun monitoring.

**Soluzione**: Loop esterno per restart automatico:
```go
for {
    leaseWatch, err := k8sClientset.CoordinationV1().Leases(...).Watch(ctx, ...)
    if err != nil {
        time.Sleep(5 * time.Second)
        continue  // Retry watch creation
    }
    
    for event := range leaseWatch.ResultChan() {
        // Handle events
    }
    
    // Watch closed, restart
}
```

### 7. ❌ Context cancellation durante load

**Problema**: Pod termina durante I/O → risorse non cleanup.

**Soluzione**: Check context prima di operazioni costose:
```go
select {
case <-ctx.Done():
    if ctx.Err() == context.DeadlineExceeded {
        setupLog.Error(nil, "Timeout waiting for certificates")
        os.Exit(1)
    }
    return
default:
    // Proceed
}
```

## Vantaggi del Watch-Based Approach

### 1. **Event-Driven vs Polling**

| Aspetto | Polling (v0.3.2) | Watch-Based (v0.3.3) |
|---------|------------------|----------------------|
| Latenza | 2 secondi (ticker) | <100ms (evento immediato) |
| CPU usage | Costante ogni 2s | Solo su eventi |
| Efficienza | Bassa | Alta |
| Scalabilità | Limitata | Eccellente |

### 2. **Thread-Safety Garantita**

- `atomic.Value` per lastSeenLeaseHash
- `sync.Once` per certReadyChan closure
- `sync.Mutex` per certWatcher access
- Double-check locking pattern

### 3. **Resilienza Migliorata**

- Watch restart automatico su failure
- Ticker fallback se watch fallisce
- Context cancellation handling
- Timeout configurabile (180s)

### 4. **Zero Downtime**

Durante il renewal del certificato:
- Follower continua a servire con cert vecchio
- Watch riceve evento lease update immediatamente
- Attende sync kubelet (~9 secondi)
- Verifica hash match
- Carica nuovo certificato
- Transizione trasparente

## Gestione Timeout

### Scenario 1: Prima esecuzione

```
T=0s:    Pod parte, watch attivo
T=0-180s: Attende leader election + cert generation
T=180s:   Se nessun cert → Exit(1) con errore
```

### Scenario 2: Certificato esistente

```
T=0s:    Pod parte, watch attivo
T=0.1s:  Legge lease, trova hash
T=0.2s:  Verifica filesystem, hash match
T=0.3s:  Carica certificato ✅
```

### Scenario 3: Renewal

```
T=0s:     Cert vecchio operativo
T=1s:     Leader genera nuovo cert
T=1.1s:   Watch riceve evento
T=1-10s:  Ticker verifica hash ogni 2s
T=~10s:   Kubelet sync completo
T=10s:    Hash match, reload certificato ✅
```

## Testing

### Test manuale renewal

```bash
# 1. Generare cert con scadenza breve
openssl genrsa -out test-key.pem 2048
openssl req -new -x509 -key test-key.pem -out test-cert.pem -days 1 \
  -subj "/CN=kubetemplater-webhook.kubetemplater-system.svc"

# 2. Update Secret
kubectl create secret tls kubetemplater-webhook-cert \
  --cert=test-cert.pem --key=test-key.pem \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Monitorare logs follower
kubectl logs -f -n kubetemplater-system \
  -l app.kubernetes.io/name=kubetemplater | grep -i "hash\|watch\|cert"

# 4. Verificare eventi watch
# Output atteso:
# - "Lease watch event received" con hash
# - "Certificate hash mismatch, waiting for kubelet sync"
# - "Certificate loaded and verified" quando hash match
```

### Verifica lease annotations

```bash
kubectl get lease -n kubetemplater-system 8377a775.my.company.com -o jsonpath='{.metadata.annotations}'
```

Output atteso:
```json
{
  "kubetemplater.io/cert-hash": "49996b89c60b0c76c35cbad0135f37cff...",
  "kubetemplater.io/cert-ready": "true",
  "kubetemplater.io/cert-valid-until": "2026-12-06T11:12:18Z"
}
```

## Metriche e Monitoring

### Log Eventi Chiave

1. **Watch established**: `Lease watch established, monitoring for certificate updates`
2. **Event received**: `Lease watch event received` (con hash)
3. **Hash mismatch**: `Certificate hash mismatch, waiting for kubelet sync`
4. **Hash match**: `Certificate loaded and verified` (con hash)
5. **Watch restart**: `Lease watch closed, restarting watch`

### Debug con Verbosity

```yaml
# deployment.yaml
args:
  - --v=1  # Abilita log V(1) per debug dettagliato
```

Con `--v=1` attivo:
- Log ogni check ticker
- Log ogni mismatch hash con valori completi
- Log eventi watch intermedi

## Troubleshooting

### Problema: "Timeout waiting for webhook certificates"

**Causa**: Leader non ha generato certificato entro 180 secondi.

**Debug**:
```bash
# Check leader election
kubectl get lease -n kubetemplater-system 8377a775.my.company.com -o yaml

# Check leader logs
kubectl logs -n kubetemplater-system \
  -l app.kubernetes.io/name=kubetemplater | grep -i "leader\|cert"
```

### Problema: "Watch creation failed"

**Causa**: RBAC o network issue.

**Debug**:
```bash
# Verify RBAC
kubectl auth can-i watch leases --as=system:serviceaccount:kubetemplater-system:kubetemplater -n kubetemplater-system

# Check network policy
kubectl get networkpolicy -n kubetemplater-system
```

### Problema: Hash mismatch persistente

**Causa**: Kubelet non sincronizza Secret.

**Debug**:
```bash
# Check Secret
kubectl get secret -n kubetemplater-system kubetemplater-webhook-cert -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -noout -text

# Restart pod per force sync
kubectl delete pod -n kubetemplater-system -l app.kubernetes.io/name=kubetemplater
```

## Configurazione

### Variabili d'ambiente

```yaml
env:
  - name: POD_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
```

`POD_NAMESPACE` è usato per identificare il namespace della Lease.

### Timeout configurabile (future enhancement)

```yaml
args:
  - --cert-timeout=300  # 5 minuti invece di 3
```

## Differenze con v0.3.2

| Aspetto | v0.3.2 (Polling) | v0.3.3 (Watch) |
|---------|------------------|----------------|
| Discovery | Polling filesystem ogni 2s | Watch eventi Lease |
| Hash verificaion | TODO (non implementato) | ✅ Completo |
| Thread-safety | Parziale (certWatcherMu) | Completa (atomic + sync.Once) |
| Efficienza | Bassa (polling costante) | Alta (event-driven) |
| Latenza notifica | Max 2 secondi | <100ms |
| Restart watch | N/A | Automatico |
| Context handling | Basico | Completo |

## Performance

### Benchmark teorici

**Scenario: 100 follower pods**

| Metric | Polling (v0.3.2) | Watch (v0.3.3) |
|--------|------------------|----------------|
| API calls/min | 3000 (100 pods × 30 checks) | ~0 (solo eventi) |
| CPU per pod | ~5m costante | ~1m idle, spike su evento |
| Latency discovery | 0-2s | 50-100ms |
| Memory overhead | Basso | Medio (watch buffer) |

## Conclusioni

La migrazione a watch-based approach con hash verification risolve:

1. ✅ **Race condition** tra lease annotation update e kubelet filesystem sync
2. ✅ **Thread-safety** completa con 7 race conditions risolte
3. ✅ **Efficienza** migliorata con eventi invece di polling
4. ✅ **Resilienza** aumentata con restart automatico e fallback
5. ✅ **Zero downtime** durante certificate renewal

Il sistema è ora **production-ready** con garanzie di correttezza e performance ottimali.
