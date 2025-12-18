# Security Scanning Pipeline

Questo progetto implementa una pipeline di sicurezza completa con scanning multi-layer.

## 🔍 Scanning Tools Implementati

### 1. **Trivy Filesystem Scan**
**Scopo**: Scansione dipendenze e codice sorgente  
**Quando**: Prima del build Docker  
**Cosa rileva**:
- Vulnerabilità in `go.mod` e dipendenze Go
- Vulnerabilità in librerie vendored
- Misconfigurations in file YAML/JSON
- Secrets hardcoded nel codice

**Esempio output**:
```
go.mod (gomod)
├── golang.org/x/net v0.0.0-20210520170846-37e1c6afe023
│   └── CVE-2021-33194 (HIGH)
```

### 2. **GoSec - Static Security Analysis**
**Scopo**: Analisi statica del codice Go per problemi di sicurezza  
**Quando**: Prima del build Docker  
**Cosa rileva**:
- SQL injection vulnerabilities
- Command injection (G204)
- Weak cryptographic primitives (MD5, SHA1, DES, RC4)
- Insecure random number generation
- Poor file permissions (G301, G302, G306)
- Path traversal (G304, G305)
- Hardcoded credentials (G101)
- Bad TLS configurations (G402)
- Unsafe memory operations (G103)

**Esempio output**:
```
[G401] Use of weak cryptographic primitive
  > crypto/md5
  line 45: hash := md5.New()
```

### 3. **govulncheck - Go Vulnerability Database**
**Scopo**: Verifica vulnerabilità note nella stdlib Go e nei moduli  
**Quando**: Prima del build Docker  
**Cosa rileva**:
- CVE noti nella versione Go usata
- Vulnerabilità in dipendenze dirette e transitive
- Chiamate a funzioni vulnerabili nel codice
- Vulnerabilità già fixate nelle versioni successive

**Esempio output**:
```
Vulnerability #1: GO-2022-0969
  Package: golang.org/x/net
  Found in: golang.org/x/net@v0.0.0-20210520170846
  Fixed in: golang.org/x/net@v0.0.0-20220906165146
  Call stacks in your code:
    main.go:123: http.Get()
```

### 4. **golangci-lint - Code Quality & Security**
**Scopo**: Linting completo con 20+ linters inclusi gosec  
**Quando**: Prima del build Docker  
**Cosa rileva**:
- Errori di programmazione comuni
- Code smells e anti-patterns
- Performance issues
- Tutti i controlli di gosec
- Unused code e imports
- Error handling mancante
- Race conditions potenziali

**Configurazione**: `.golangci.yml` con focus su sicurezza

### 5. **Trivy Image Scan**
**Scopo**: Scansione dell'immagine Docker finale  
**Quando**: Dopo il build e push Docker  
**Cosa rileva**:
- Vulnerabilità nell'OS base (Alpine/Ubuntu)
- Vulnerabilità in pacchetti di sistema
- Vulnerabilità nell'applicazione Go compilata
- Misconfigurations Docker
- Secrets nell'immagine
- Malware (se abilitato)

**Scanning multipli**:
- Vulnerabilità: `trivy image --scanners vuln`
- Configurazioni: `trivy image --scanners config`
- Secrets: `trivy image --scanners secret`

## 📊 Output Formats

### SARIF (Static Analysis Results Interchange Format)
Tutti gli scanner producono output in formato SARIF per integrazione con Azure DevOps:
- `trivy-fs-results.sarif` - Filesystem scan
- `gosec-results.sarif` - GoSec scan
- `golangci-lint-results.sarif` - Linting results
- `trivy-image-results.sarif` - Docker image scan

### JSON
- `govulncheck-results.json` - Vulnerabilità Go dettagliate

### Artifacts Azure DevOps
Tutti i risultati vengono pubblicati come artifacts:
- `security-scan-results` - Tutti gli scan del codice sorgente
- `trivy-image-scan-results` - Scan dell'immagine Docker

## 🚨 Severity Levels

Tutti gli scanner sono configurati per reportare **HIGH** e **CRITICAL**:
- **CRITICAL**: Vulnerabilità che permettono exploit immediato
- **HIGH**: Vulnerabilità serie che richiedono attenzione urgente
- **MEDIUM**: Problemi di sicurezza da fixare (non bloccanti)
- **LOW**: Best practices e hardening (informativo)

## 🔄 Pipeline Flow

```
1. Checkout Code
   ↓
2. Install Scanning Tools
   ↓
3. Trivy Filesystem Scan (codice + dipendenze)
   ↓
4. GoSec Static Analysis (codice Go)
   ↓
5. govulncheck (vulnerabilità Go)
   ↓
6. golangci-lint (quality + security)
   ↓
7. Publish Security Scan Results
   ↓
8. Docker Build
   ↓
9. Docker Push to ACR
   ↓
10. Trivy Image Scan (immagine finale)
    ↓
11. Publish Image Scan Results
```

## 🛠️ Esecuzione Locale

### Trivy Filesystem
```bash
trivy fs --severity HIGH,CRITICAL .
```

### GoSec
```bash
gosec -fmt=text ./...
```

### govulncheck
```bash
govulncheck ./...
```

### golangci-lint
```bash
golangci-lint run ./...
```

### Trivy Image (dopo build locale)
```bash
docker build -t kubetemplater:test .
trivy image --severity HIGH,CRITICAL kubetemplater:test
```

## 📈 Continuous Improvement

### False Positives
Se uno scanner genera falsi positivi:

**GoSec**: Aggiungi commento nel codice
```go
// #nosec G304 - File path is validated by controller-runtime
data, err := os.ReadFile(certPath)
```

**golangci-lint**: Aggiungi a `.golangci.yml` → `issues.exclude-rules`

**Trivy**: Crea `.trivyignore`
```
# False positive in base image
CVE-2023-12345
```

### Aggiornamento Tools
Tutti i tools sono installati dalla versione latest nella pipeline:
- Trivy: da repository APT ufficiale
- GoSec: `go install @latest`
- govulncheck: `go install @latest`
- golangci-lint: versione pinned `v1.61.0`

## 🔐 Best Practices

1. **Non committare mai**:
   - Password, API keys, tokens
   - Certificate privati
   - Credenziali database
   
2. **Usare Kubernetes Secrets** per dati sensibili

3. **Aggiornare regolarmente**:
   - Versione Go (attualmente 1.23)
   - Dipendenze in `go.mod`
   - Base image Docker (Alpine)

4. **Revieware i risultati** degli scan prima del merge

5. **Fixare CRITICAL** prima del deployment in produzione

## 📚 Risorse

- [Trivy Documentation](https://aquasecurity.github.io/trivy/)
- [GoSec Rules](https://github.com/securego/gosec#available-rules)
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [SARIF Format](https://sarifweb.azurewebsites.net/)
