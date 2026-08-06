# =============================================================================
# Sub2API migration helper (Windows PowerShell)
# =============================================================================
# Usage:
#   ./scripts/migrate.ps1 export [output-directory]
#   ./scripts/migrate.ps1 import [bundle.tar.gz]
#
# PGHOST/DATABASE_HOST must describe where PostgreSQL is reachable from the
# process running this script. The default host port is 5433; use PGPORT=5432
# only inside the Compose network. .NET gzip streams are used, so gzip/gunzip is
# not required on Windows.
# =============================================================================

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('export', 'import', '')]
    [string]$Action = '',
    [Parameter(Position = 1)]
    [string]$Path = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script:WorkDir = $null
$repoRoot = Split-Path -Parent $PSScriptRoot
if ($env:SUB2API_ENV_FILE) {
    $script:EnvFile = $env:SUB2API_ENV_FILE
} elseif (Test-Path -LiteralPath (Join-Path (Get-Location) '.env') -PathType Leaf) {
    $script:EnvFile = Join-Path (Get-Location) '.env'
} elseif (Test-Path -LiteralPath (Join-Path $repoRoot 'deploy/.env') -PathType Leaf) {
    $script:EnvFile = Join-Path $repoRoot 'deploy/.env'
} else {
    $script:EnvFile = Join-Path (Get-Location) '.env'
}
$script:EnvTargetFile = if (Test-Path -LiteralPath (Join-Path $repoRoot 'deploy') -PathType Container) {
    Join-Path $repoRoot 'deploy/.env'
} else {
    Join-Path (Get-Location) '.env'
}
$script:ConfigFile = if (Test-Path -LiteralPath (Join-Path $repoRoot 'deploy/config.yaml') -PathType Leaf) {
    Join-Path $repoRoot 'deploy/config.yaml'
} else {
    Join-Path (Get-Location) 'config.yaml'
}

function Info([string]$Message)    { Write-Host "[INFO] $Message" -ForegroundColor Blue }
function Success([string]$Message) { Write-Host "[SUCCESS] $Message" -ForegroundColor Green }
function Warn([string]$Message)    { Write-Host "[WARNING] $Message" -ForegroundColor Yellow }
function Die([string]$Message)     { throw $Message }

function Load-PgEnv {
    $keys = @(
        'PGHOST', 'PGPORT', 'PGUSER', 'PGDATABASE', 'PGPASSWORD', 'PGPASSFILE', 'MIGRATION_DB_HOST', 'MIGRATION_DB_PORT', 'POSTGRES_HOST_PORT',
        'DATABASE_HOST', 'DATABASE_PORT', 'DATABASE_USER', 'DATABASE_DBNAME', 'DATABASE_PASSWORD',
        'POSTGRES_USER', 'POSTGRES_DB', 'POSTGRES_PASSWORD'
    )

    if (Test-Path -LiteralPath $script:EnvFile -PathType Leaf) {
        foreach ($line in (Get-Content -LiteralPath $script:EnvFile)) {
            $trimmed = $line.Trim()
            if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
            if ($trimmed -notmatch '^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') { continue }
            $name = $matches[1]
            if ($keys -notcontains $name) { continue }
            if ([string]::IsNullOrEmpty([Environment]::GetEnvironmentVariable($name, 'Process'))) {
                $value = $matches[2].Trim()
                if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
                    $value = $value.Substring(1, $value.Length - 2)
                }
                [Environment]::SetEnvironmentVariable($name, $value, 'Process')
            }
        }
    }

    if (-not $env:PGHOST) {
        $env:PGHOST = if ($env:MIGRATION_DB_HOST) { $env:MIGRATION_DB_HOST } else { '127.0.0.1' }
    }
    if (-not $env:PGPORT) {
        $env:PGPORT = if ($env:MIGRATION_DB_PORT) { $env:MIGRATION_DB_PORT } elseif ($env:POSTGRES_HOST_PORT) { $env:POSTGRES_HOST_PORT } else { '5433' }
    }
    if (-not $env:PGUSER) {
        $env:PGUSER = if ($env:DATABASE_USER) { $env:DATABASE_USER } elseif ($env:POSTGRES_USER) { $env:POSTGRES_USER } else { 'postgres' }
    }
    if (-not $env:PGDATABASE) {
        $env:PGDATABASE = if ($env:DATABASE_DBNAME) { $env:DATABASE_DBNAME } elseif ($env:POSTGRES_DB) { $env:POSTGRES_DB } else { 'sub2api' }
    }
    if (-not $env:PGPASSWORD) {
        if ($env:POSTGRES_PASSWORD) { $env:PGPASSWORD = $env:POSTGRES_PASSWORD }
        elseif ($env:DATABASE_PASSWORD) { $env:PGPASSWORD = $env:DATABASE_PASSWORD }
    }
}

function Validate-PgEnv {
    if (-not $env:PGHOST) { Die 'PGHOST/MIGRATION_DB_HOST is empty' }
    if (-not $env:PGUSER) { Die 'PGUSER/DATABASE_USER or POSTGRES_USER is required' }
    if (-not $env:PGDATABASE) { Die 'PGDATABASE/DATABASE_DBNAME or POSTGRES_DB is required' }
    if ($env:PGPORT -notmatch '^\d+$') { Die 'PGPORT/MIGRATION_DB_PORT must be a number' }
    if (-not $env:PGPASSWORD -and -not $env:PGPASSFILE) {
        Die 'set PGPASSWORD, PGPASSFILE, or a password variable before running the migration'
    }
}

function Get-RequiredCommand([string]$Name) {
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $command) { Die "$Name not found; install PostgreSQL client tools and tar" }
    return $command.Source
}

function Compress-File([string]$InputPath, [string]$OutputPath) {
    $inputStream = $null
    $outputStream = $null
    $gzipStream = $null
    try {
        $inputStream = [System.IO.File]::OpenRead($InputPath)
        $outputStream = [System.IO.File]::Create($OutputPath)
        $gzipStream = [System.IO.Compression.GZipStream]::new($outputStream, [System.IO.Compression.CompressionMode]::Compress)
        $inputStream.CopyTo($gzipStream)
    }
    finally {
        if ($gzipStream) { $gzipStream.Dispose() }
        elseif ($outputStream) { $outputStream.Dispose() }
        if ($inputStream) { $inputStream.Dispose() }
    }
}

function Expand-File([string]$InputPath, [string]$OutputPath) {
    $inputStream = $null
    $outputStream = $null
    $gzipStream = $null
    try {
        $inputStream = [System.IO.File]::OpenRead($InputPath)
        $gzipStream = [System.IO.Compression.GZipStream]::new($inputStream, [System.IO.Compression.CompressionMode]::Decompress)
        $outputStream = [System.IO.File]::Create($OutputPath)
        $gzipStream.CopyTo($outputStream)
    }
    finally {
        if ($outputStream) { $outputStream.Dispose() }
        if ($gzipStream) { $gzipStream.Dispose() }
        elseif ($inputStream) { $inputStream.Dispose() }
    }
}

function New-WorkDirectory([string]$Prefix) {
    $path = Join-Path ([System.IO.Path]::GetTempPath()) "$Prefix-$([guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Path $path -Force | Out-Null
    $script:WorkDir = $path
    return $path
}

function Remove-WorkDirectory {
    if ($script:WorkDir -and (Test-Path -LiteralPath $script:WorkDir)) {
        Remove-Item -LiteralPath $script:WorkDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    $script:WorkDir = $null
}

function Validate-Archive([string]$ArchivePath, [string]$TarPath) {
    $members = @(& $TarPath '-tf' $ArchivePath)
    if ($LASTEXITCODE -ne 0) { Die "unable to read archive: $ArchivePath" }
    $allowed = @('db.sql.gz', '.env', 'config.yaml', 'README.md')
    $dbSeen = $false

    foreach ($rawMember in $members) {
        $member = ([string]$rawMember).Trim()
        while ($member.StartsWith('./')) { $member = $member.Substring(2) }
        if (-not $member) { continue }
        if ($member -match '(^[\\/]|^[A-Za-z]:|(^|[\\/])\.\.([\\/]|$))') {
            Die "unsafe archive path: $rawMember"
        }
        if ($allowed -notcontains $member) { Die "unexpected archive member: $rawMember" }
        if ($member -eq 'db.sql.gz') { $dbSeen = $true }
    }

    $verbose = @(& $TarPath '-tvf' $ArchivePath)
    if ($LASTEXITCODE -ne 0) { Die "unable to inspect archive: $ArchivePath" }
    foreach ($entry in $verbose) {
        $line = ([string]$entry).TrimStart()
        if ($line -match '^[dlhpcbsp]') { Die 'archive contains a non-regular file' }
    }
    if (-not $dbSeen) { Die 'archive does not contain db.sql.gz' }
}

function Assert-RegularFile([string]$FilePath) {
    if (-not (Test-Path -LiteralPath $FilePath -PathType Leaf)) { Die "archive file missing: $FilePath" }
    $item = Get-Item -LiteralPath $FilePath -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { Die "archive member is a link: $FilePath" }
}

function Export-Bundle {
    param([string]$OutDir)
    Load-PgEnv
    Validate-PgEnv
    $pgDump = Get-RequiredCommand 'pg_dump'
    $tar = Get-RequiredCommand 'tar'
    if (-not $OutDir) { $OutDir = '.' }
    $outPath = [System.IO.Path]::GetFullPath($OutDir)
    New-Item -ItemType Directory -Path $outPath -Force | Out-Null
    $work = New-WorkDirectory 'sub2api-migrate'
    $ts = Get-Date -Format 'yyyyMMdd_HHmmss'
    $dbSql = Join-Path $work 'db.sql'
    $tarFile = Join-Path $work "migration-bundle-$ts.tar"
    $bundle = Join-Path $outPath "migration-bundle-$ts.tar.gz"

    try {
        $pgArgs = @('-h', $env:PGHOST, '-p', $env:PGPORT, '-U', $env:PGUSER, '--no-password', $env:PGDATABASE,
            '--format=plain', '--no-owner', '--no-acl', '--clean', '--if-exists', '--file', $dbSql)
        Info "dumping database $env:PGDATABASE from $env:PGHOST ..."
        & $pgDump @pgArgs
        if ($LASTEXITCODE -ne 0) { Die 'pg_dump failed; check PGHOST, credentials, and database availability' }
        Compress-File $dbSql (Join-Path $work 'db.sql.gz')

        if (Test-Path -LiteralPath $script:EnvFile -PathType Leaf) { Copy-Item -LiteralPath $script:EnvFile -Destination (Join-Path $work '.env') }
        if (Test-Path -LiteralPath $script:ConfigFile -PathType Leaf) { Copy-Item -LiteralPath $script:ConfigFile -Destination (Join-Path $work 'config.yaml') }
        @'
# Sub2API migration bundle
Restore requirements:
  - PostgreSQL client tools (pg_dump/psql) and tar
  - Set target PGHOST/PGPORT/PGUSER/PGDATABASE and password before import
  - Run: ./scripts/migrate.ps1 import migration-bundle-<timestamp>.tar.gz
  - Start: docker compose -f deploy/docker-compose.local.yml up -d
'@ | Set-Content -LiteralPath (Join-Path $work 'README.md') -Encoding UTF8

        $archiveFiles = @('db.sql.gz', 'README.md')
        if (Test-Path -LiteralPath (Join-Path $work '.env') -PathType Leaf) { $archiveFiles += '.env' }
        if (Test-Path -LiteralPath (Join-Path $work 'config.yaml') -PathType Leaf) { $archiveFiles += 'config.yaml' }
        & $tar '-cf' $tarFile '-C' $work @archiveFiles
        if ($LASTEXITCODE -ne 0) { Die 'tar failed while creating the bundle' }
        Compress-File $tarFile $bundle
        Success "exported bundle: $bundle"
    }
    finally { Remove-WorkDirectory }
}

function Import-Bundle {
    param([string]$Bundle)
    if (-not $Bundle) { Die 'usage: ./scripts/migrate.ps1 import <bundle.tar.gz>' }
    $bundlePath = (Resolve-Path -LiteralPath $Bundle -ErrorAction SilentlyContinue).Path
    if (-not $bundlePath) { Die "bundle not found: $Bundle" }
    Load-PgEnv
    Validate-PgEnv
    $psql = Get-RequiredCommand 'psql'
    $tar = Get-RequiredCommand 'tar'
    $work = New-WorkDirectory 'sub2api-restore'
    $archive = Join-Path $work 'migration-bundle.tar'
    $files = Join-Path $work 'files'
    New-Item -ItemType Directory -Path $files -Force | Out-Null

    try {
        Expand-File $bundlePath $archive
        Validate-Archive $archive $tar
        & $tar '-xf' $archive '-C' $files
        if ($LASTEXITCODE -ne 0) { Die 'tar failed while extracting the bundle' }
        $dbGzip = Join-Path $files 'db.sql.gz'
        Assert-RegularFile $dbGzip
        $dbSql = Join-Path $work 'db.sql'
        Expand-File $dbGzip $dbSql

        $envBundle = Join-Path $files '.env'
        if (Test-Path -LiteralPath $envBundle -PathType Leaf) { Copy-Item -LiteralPath $envBundle -Destination $script:EnvTargetFile; Info "restored $script:EnvTargetFile" }
        $configBundle = Join-Path $files 'config.yaml'
        if (Test-Path -LiteralPath $configBundle -PathType Leaf) { Copy-Item -LiteralPath $configBundle -Destination $script:ConfigFile; Info "restored $script:ConfigFile" }

        Info "restoring database $env:PGDATABASE to $env:PGHOST ..."
        $psqlArgs = @('-h', $env:PGHOST, '-p', $env:PGPORT, '-U', $env:PGUSER, '--no-password', $env:PGDATABASE,
            '-v', 'ON_ERROR_STOP=1', '--single-transaction', '-f', $dbSql)
        & $psql @psqlArgs
        if ($LASTEXITCODE -ne 0) { Die 'database restore failed; SQL execution stopped at the first error' }
        Success 'database restored'
        Warn 'now run: docker compose -f deploy/docker-compose.local.yml up -d'
    }
    finally { Remove-WorkDirectory }
}

try {
    switch ($Action) {
        'export' { Export-Bundle -OutDir $Path }
        'import' { Import-Bundle -Bundle $Path }
        default  { Write-Host './scripts/migrate.ps1 {export [out_dir] | import [bundle.tar.gz]}'; exit 1 }
    }
}
catch {
    Write-Host "[ERROR] $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
