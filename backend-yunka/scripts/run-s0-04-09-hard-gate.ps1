[CmdletBinding()]
param(
    [switch] $VerifyOnly
)

$ErrorActionPreference = 'Stop'

function Invoke-Checked {
    param(
        [Parameter(Mandatory)] [scriptblock] $Command,
        [Parameter(Mandatory)] [string] $Description
    )

    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Description failed with exit code $LASTEXITCODE"
    }
}

$backendRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$repositoryRoot = (Resolve-Path (Join-Path $backendRoot '..')).Path
$yunkaRoot = Join-Path $repositoryRoot 'third_party/yunka'
$yunkaApp = Join-Path $yunkaRoot 'app'
$protoc = Join-Path $backendRoot '.tools/protoc-3.21.12/bin/protoc.exe'
$protocGenGo = Join-Path $backendRoot '.tools/bin/protoc-gen-go.exe'
$protocGenGoGRPC = Join-Path $backendRoot '.tools/bin/protoc-gen-go-grpc.exe'
$protoPath = Join-Path $yunkaRoot 'contracts/proto'
$expectedYunkaRevision = '9a51562aa7bcef42f6861bd91abd30aae13ed6ef'

function Assert-LockedYunkaRevision {
    $gitlink = & git -C $repositoryRoot ls-tree HEAD -- third_party/yunka
    if ($LASTEXITCODE -ne 0) {
        throw 'Unable to resolve the Yunka gitlink from repository HEAD.'
    }
    $gitlinkFields = @(([string]($gitlink -join "`n")).Trim() -split '\s+')
    if ($gitlinkFields.Count -ne 4 -or $gitlinkFields[0] -cne '160000' -or $gitlinkFields[1] -cne 'commit' -or $gitlinkFields[2] -cne $expectedYunkaRevision -or $gitlinkFields[3] -cne 'third_party/yunka') {
        throw 'Repository HEAD does not pin third_party/yunka to the required revision.'
    }
    $submoduleHead = & git -C $yunkaRoot rev-parse HEAD
    if ($LASTEXITCODE -ne 0 -or [string]($submoduleHead -join "`n").Trim() -cne $expectedYunkaRevision) {
        throw 'Materialized third_party/yunka HEAD does not match the required revision.'
    }
    $submoduleStatus = & git -C $yunkaRoot status --porcelain
    if ($LASTEXITCODE -ne 0 -or -not [string]::IsNullOrWhiteSpace([string]($submoduleStatus -join "`n"))) {
        throw 'Materialized third_party/yunka is not clean.'
    }
}

Assert-LockedYunkaRevision

if (-not (Test-Path (Join-Path $yunkaRoot 'framework/go.mod'))) {
    throw 'The pinned Yunka submodule is not materialized at third_party/yunka.'
}
if (-not (Test-Path $protoc)) {
    throw 'The pinned protoc executable is not available under backend-yunka/.tools.'
}
if (-not (Test-Path $protocGenGo) -or -not (Test-Path $protocGenGoGRPC)) {
    throw 'The pinned protoc Go plugins are not available under backend-yunka/.tools/bin.'
}

$hadProtocGenGo = Test-Path Env:PROTOC_GEN_GO
$previousProtocGenGo = $env:PROTOC_GEN_GO
$hadProtocGenGoGRPC = Test-Path Env:PROTOC_GEN_GO_GRPC
$previousProtocGenGoGRPC = $env:PROTOC_GEN_GO_GRPC
try {
    $env:PROTOC_GEN_GO = (Resolve-Path $protocGenGo).Path
    $env:PROTOC_GEN_GO_GRPC = (Resolve-Path $protocGenGoGRPC).Path
    if ($VerifyOnly) {
        return
    }

    Push-Location $backendRoot
    try {
        Invoke-Checked { go test ./internal/bffhttp -run 'TestBFFHTTPPersistsAcceptedAssertionWithoutAssertionContents|TestBFFHTTPPersistsRejectedAssertionAsAnonymous|TestBFFHTTPRejectsExpiredAndRequestTamperedAssertionsWithoutMutation' -count=1 } 'BFF trusted-identity hard gate'
        Invoke-Checked { go test ./internal/serviceauth -run 'TestIssueAndAuthenticateCreatesServicePrincipalWithoutPersistingCredential|TestCredentialRevocationCommitsWithItsAuditEntry|TestCredentialRevocationRollsBackWhenAuditAppendFails|TestGRPCServiceCredentialAdapterAttachesServicePrincipalAndTrace' -count=1 } 'service-identity hard gate'
        Invoke-Checked { go test ./internal/bootstrap -run 'TestProductionAuthorizationMatrixRejectsRESTAndMCPWithoutSideEffects|TestPostAuthTransportMatrixAllowsGateWithEquivalentSQLiteAndOutboxEffects|TestProductionServiceGrantResolverAllowsOnlyItsGrantedProjectOverGRPC|TestProductionRevisionConflictMatrixPreservesExactlyOneCrossTransportWrite' -count=1 } 'production authorization and cross-transport concurrency hard gate'
        Invoke-Checked { go test ./internal/configapplication -run 'TestOperationsChangeCompareAndRollbackUseTrustedScopeAuditAndImmutableChain|TestOperationsStageOutboxOnlyForCommittedConfigurationMutations|TestOperationsOutboxFailureRollsBackConfigurationRevisionAndAudit|TestOperationsRejectsConflictAndAuditFailureWithoutRevisionOrAuditSideEffects|TestOperationsRollbackAuditFailureLeavesNoRevisionAuditOrOutboxSideEffects' -count=1 } 'configuration revision hard gate'
        Invoke-Checked { go test ./internal/audit -run 'TestRecordingExecutorPersistsFailureOnlyAfterBusinessRollback|TestRecordingExecutorKeepsAuthorizationDenialWhenAuditFails|TestAuditSanitizerRedactsSensitiveKeysAndValuesAcrossWritePaths' -count=1 } 'audit hard gate'
        Invoke-Checked { go test ./internal/configrevision -run 'TestSQLiteStorePersistsImmutableRevisionChainAcrossReopen|TestSQLiteStoreRejectsStaleParentDuplicateIDAndConcurrentAppendWithoutGaps' -count=1 } 'SQLite durability hard gate'
        Invoke-Checked { go test ./... -count=1 } 'full Go test suite'
        Invoke-Checked { go vet ./... } 'Go vet'
    }
    finally {
        Pop-Location
    }

    Push-Location $yunkaApp
    try {
        Invoke-Checked { go run ./cmd check --root $backendRoot --full --protoc $protoc --proto-path $protoPath } 'pinned Yunka contract check'
    }
    finally {
        Pop-Location
    }
}
finally {
    if ($hadProtocGenGo) {
        $env:PROTOC_GEN_GO = $previousProtocGenGo
    }
    else {
        Remove-Item Env:PROTOC_GEN_GO -ErrorAction SilentlyContinue
    }
    if ($hadProtocGenGoGRPC) {
        $env:PROTOC_GEN_GO_GRPC = $previousProtocGenGoGRPC
    }
    else {
        Remove-Item Env:PROTOC_GEN_GO_GRPC -ErrorAction SilentlyContinue
    }
}
