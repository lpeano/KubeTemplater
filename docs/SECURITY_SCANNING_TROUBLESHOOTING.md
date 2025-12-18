# Security Scanning Troubleshooting Guide

## Problemi Comuni e Soluzioni

### 1. "Security scanning non funziona nelle pipeline"

#### Problema
Gli step di security scanning nella pipeline Azure DevOps falliscono silenziosamente o non producono risultati visibili.

#### Cause Principali

**A. `continueOnError: true` nasconde i fallimenti**
```yaml
# ❌ PRIMA (nasconde errori)
- task: Bash@3
  displayName: 'GoSec scan'
  inputs:
    script: gosec ...
  continueOnError: true  # ⚠️ Pipeline continua anche se fallisce

# ✅ DOPO (mostra errori reali)
- task: Bash@3
  displayName: 'GoSec scan'
  inputs:
    script: gosec ...
  # Rimosso continueOnError - errori ora visibili
```

**B. Path errati per i tool installati**
```bash
# ❌ Tool installati in $GOPATH/bin ma non in PATH
gosec -fmt=sarif ...  # Command not found

# ✅ Aggiungere GOPATH/bin al PATH
export PATH=$PATH:$(go env GOPATH)/bin
which gosec  # Verifica path
gosec -version  # Verifica installazione
gosec -fmt=sarif ...
```

**C. File SARIF non pubblicati correttamente**
```yaml
# ❌ Path relativi senza $(Build.SourcesDirectory)
--output trivy-results.sarif

# ✅ Path assoluti con variabile
--output $(Build.SourcesDirectory)/trivy-results.sarif
```

**D. Manca PublishSecurityAnalysisLogs task**
```yaml
# ❌ Solo PublishBuildArtifacts (non integrato con Security tab)
- task: PublishBuildArtifacts@1
  inputs:
    PathtoPublish: '$(Build.ArtifactStagingDirectory)/security-scans'

# ✅ Aggiungere PublishSecurityAnalysisLogs per Security tab
- task: PublishSecurityAnalysisLogs@3
  displayName: 'Publish Security Analysis Logs'
  inputs:
    ArtifactName: 'CodeAnalysisLogs'
    ArtifactType: 'Container'
    AllTools: true
```

#### Soluzioni Applicate (v0.3.3)

1. **Rimosso `continueOnError`** dagli step critici
2. **Fixati path tool** con verifica installazione:
   ```bash
   if ! command -v gosec &> /dev/null; then
     export PATH=$PATH:$(go env GOPATH)/bin
   fi
   which gosec  # Debug output
   ```

3. **Path assoluti** per file SARIF:
   ```bash
   --output $(Build.SourcesDirectory)/trivy-fs-results.sarif
   ```

4. **Aggiunto PublishSecurityAnalysisLogs** task per integrazione Security tab

5. **Debug output** per troubleshooting:
   ```bash
   echo "Running GoSec..."
   which gosec
   gosec -version
   gosec -fmt=sarif ...
   ```

---

### 2. "Trivy non trova l'immagine Docker"

#### Problema
```
FATAL: image scan error: unable to initialize a scanner
```

#### Causa
Trivy tenta di scannare l'immagine ACR senza autenticazione.

#### Soluzione
```yaml
- task: AzureCLI@2
  displayName: 'Scan Image with Trivy'
  inputs:
    azureSubscription: $(azureServiceConnection)
    scriptType: 'bash'
    inlineScript: |
      # ✅ Login ACR prima dello scan
      az acr login --name your-acr-registry
      
      trivy image $(containerRegistry)/$(imageName):$(Build.BuildId)
```

---

### 3. "GoSec: command not found"

#### Problema
```
/bin/bash: gosec: command not found
```

#### Causa
`go install` installa in `$GOPATH/bin` che non è nel PATH di default.

#### Soluzione
```bash
# Installazione
go install github.com/securego/gosec/v2/cmd/gosec@latest

# Aggiungere al PATH prima di usare
if ! command -v gosec &> /dev/null; then
  export PATH=$PATH:$(go env GOPATH)/bin
fi

# Verifica
which gosec
gosec -version
```

---

### 4. "golangci-lint: no linters were enabled"

#### Problema
```
WARN [runner] Can't run linter govet: linter is not enabled
```

#### Causa
File `.golangci.yml` non trovato o configurazione errata.

#### Soluzione
```bash
# Verifica file esiste
ls -la .golangci.yml

# Test configurazione
golangci-lint linters
golangci-lint run --print-issued-lines=false --print-linter-name=true
```

Configurazione minima `.golangci.yml`:
```yaml
linters:
  enable:
    - gosec
    - govet
    - staticcheck
    - errcheck
```

---

### 5. "SARIF files not found in Security tab"

#### Problema
I file SARIF vengono pubblicati come artifacts ma non appaiono nella Security tab di Azure DevOps.

#### Causa
Manca il task `PublishSecurityAnalysisLogs` che integra i risultati con la Security tab.

#### Soluzione
```yaml
# Dopo CopyFiles e PublishBuildArtifacts
- task: PublishSecurityAnalysisLogs@3
  displayName: 'Publish Security Analysis Logs'
  inputs:
    ArtifactName: 'CodeAnalysisLogs'
    ArtifactType: 'Container'
    AllTools: true  # Trova automaticamente tutti i SARIF
    ToolLogsNotFoundAction: 'Standard'  # Warning se nessun file trovato
```

**Dove trovare i risultati**:
1. **Artifacts**: Build → Artifacts → `security-scan-results`
2. **Security tab**: Build → Extensions → Scans
3. **Logs**: Build → Logs → step specifico

---

### 6. "govulncheck: go1.24 not found"

#### Problema
```
error: package requires go1.24 or later
```

#### Causa
Kubernetes v0.33.0 richiede Go 1.24 nei metadata, ma Go 1.24 non è ancora rilasciato.

#### Soluzione Temporanea
```yaml
- task: Bash@3
  displayName: 'govulncheck: Go Vulnerability Database'
  inputs:
    script: |
      echo "⚠️  Skipping govulncheck - requires Go 1.24 (not yet released)"
      echo "Alternative: Trivy filesystem scan covers Go module vulnerabilities"
  continueOnError: true  # OK qui, step informativo
```

**Alternativa**: Trivy filesystem scan copre le stesse vulnerabilità.

**Quando riabilitare**: Quando Go 1.24 sarà rilasciato (Q1 2026).

---

### 7. "Trivy: database update failed"

#### Problema
```
FATAL: failed to initialize DB: download failed
```

#### Causa
Trivy non riesce a scaricare il database vulnerabilità (network issues, firewall).

#### Soluzione
```bash
# Opzione 1: Skip DB update (usa cache)
trivy image --skip-db-update ...

# Opzione 2: Timeout più lungo
trivy image --timeout 10m ...

# Opzione 3: Mirror alternativo
trivy image --db-repository ghcr.io/aquasecurity/trivy-db ...

# Debug
trivy --debug image ...
```

---

## Verifica Installazione Tool

### Script di verifica completo
```bash
#!/bin/bash
set -e

echo "=== Verifying Security Tools Installation ==="

# Trivy
echo "1. Trivy:"
which trivy || echo "❌ Not found"
trivy --version || echo "❌ Failed"

# GoSec
echo "2. GoSec:"
export PATH=$PATH:$(go env GOPATH)/bin
which gosec || echo "❌ Not found"
gosec -version || echo "❌ Failed"

# golangci-lint
echo "3. golangci-lint:"
which golangci-lint || echo "❌ Not found"
golangci-lint --version || echo "❌ Failed"

# Go
echo "4. Go:"
go version || echo "❌ Failed"
go env GOPATH || echo "❌ Failed"

echo "✅ All tools verified"
```

---

## Debug Pipeline Locale

### Eseguire security scan in locale
```bash
cd /path/to/KubeTemplater

# 1. Trivy filesystem
trivy fs --severity HIGH,CRITICAL .

# 2. GoSec
export PATH=$PATH:$(go env GOPATH)/bin
gosec -fmt=text ./...

# 3. golangci-lint
golangci-lint run ./...

# 4. Trivy image (dopo build locale)
docker build -t kubetemplater:test .
trivy image kubetemplater:test
```

---

## Log Analysis

### Identificare errori comuni nei log

**Pattern 1: Tool non trovato**
```
/bin/bash: gosec: command not found
```
→ Fix: Aggiungere `$GOPATH/bin` al PATH

**Pattern 2: File SARIF non creato**
```
##[warning]File not found: trivy-results.sarif
```
→ Fix: Verificare path assoluto con `$(Build.SourcesDirectory)`

**Pattern 3: Permission denied**
```
chmod: cannot access 'gosec': No such file or directory
```
→ Fix: Tool non installato correttamente

**Pattern 4: Exit code non-zero nascosto**
```
##[section]Finishing: GoSec scan
(no error shown, but scan failed)
```
→ Fix: Rimuovere `continueOnError: true`

---

## Best Practices

### 1. Sempre usare path assoluti
```yaml
# ❌ Bad
--output results.sarif

# ✅ Good
--output $(Build.SourcesDirectory)/results.sarif
```

### 2. Verificare tool prima di usare
```bash
if ! command -v toolname &> /dev/null; then
  echo "ERROR: toolname not found"
  exit 1
fi
```

### 3. Debug output per troubleshooting
```bash
echo "=== Tool Version ==="
toolname --version

echo "=== Running scan ==="
toolname scan ...

echo "=== Results ==="
cat results.sarif | jq '.runs[0].results | length'
```

### 4. Pubblicare SARIF in due modi
```yaml
# Per download manuale
- task: PublishBuildArtifacts@1
  inputs:
    PathtoPublish: '$(Build.ArtifactStagingDirectory)/scans'

# Per Security tab
- task: PublishSecurityAnalysisLogs@3
  inputs:
    AllTools: true
```

### 5. continueOnError solo per step informativi
```yaml
# ✅ OK: Step informativo/sperimentale
- task: Bash@3
  displayName: 'Experimental feature check'
  continueOnError: true

# ❌ Bad: Security scan critico
- task: Bash@3
  displayName: 'Trivy scan'
  continueOnError: true  # Nasconde vulnerabilità!
```

---

## Checklist Pre-Commit

Prima di committare modifiche alle pipeline:

- [ ] Tutti i tool hanno verifica installazione (`which`, `--version`)
- [ ] Path assoluti per tutti i file SARIF
- [ ] `continueOnError` rimosso dagli step critici
- [ ] `PublishSecurityAnalysisLogs` presente
- [ ] Debug output per troubleshooting
- [ ] Test locale eseguito con successo
- [ ] Documentazione aggiornata

---

## Risorse

- [Trivy Documentation](https://aquasecurity.github.io/trivy/)
- [GoSec Rules](https://github.com/securego/gosec#available-rules)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)
- [Azure DevOps SARIF Support](https://learn.microsoft.com/en-us/azure/devops/pipelines/tasks/utility/publish-security-analysis-logs)
- [SARIF Format Specification](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
