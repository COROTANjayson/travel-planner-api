param(
    [ValidateSet('setup', 'start', 'stop', 'status')]
    [string]$Action = 'status'
)

$ErrorActionPreference = 'Stop'
$projectDir = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$localDir = Join-Path $projectDir '.local'
$dataDir = Join-Path $localDir 'postgres'
$envFile = Join-Path $projectDir '.env'
$postgresBin = 'C:\Program Files\PostgreSQL\18\bin'
$pgCtl = Join-Path $postgresBin 'pg_ctl.exe'
if (-not (Test-Path -LiteralPath $pgCtl)) {
    throw 'PostgreSQL 18 tools were not found. Update $postgresBin in this script to your installation path.'
}

function Start-LocalDatabase {
    & $pgCtl -D $dataDir status *> $null
    if ($LASTEXITCODE -eq 0) {
        Write-Output 'Project PostgreSQL is already running on port 55432.'
        return
    }
    $logPath = Join-Path $localDir 'postgres.log'
    $arguments = '-D "{0}" -l "{1}" -o "-h 127.0.0.1 -p 55432" -w start' -f $dataDir, $logPath
    $process = Start-Process -FilePath $pgCtl -ArgumentList $arguments -WindowStyle Hidden -PassThru
    # Start-Process -Wait also waits for PostgreSQL's long-lived child process.
    if (-not $process.WaitForExit(20000)) { throw 'Timed out starting project PostgreSQL. Check .local/postgres.log.' }
    if ($process.ExitCode -ne 0) { throw 'Could not start project PostgreSQL. Check .local/postgres.log and whether port 55432 is available.' }
}

switch ($Action) {
    'setup' {
        if (Test-Path -LiteralPath (Join-Path $dataDir 'PG_VERSION')) {
            if (-not (Test-Path -LiteralPath $envFile)) { throw 'Database already exists but .env is missing. Restore its connection settings before continuing.' }
        } else {
            if (Test-Path -LiteralPath $envFile) { throw '.env already exists. Preserve its settings and configure an existing database, or move it aside before a new local setup.' }
            New-Item -ItemType Directory -Path $localDir -Force | Out-Null
            $randomBytes = New-Object byte[] 24
            $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
            try { $generator.GetBytes($randomBytes) } finally { $generator.Dispose() }
            $password = [System.BitConverter]::ToString($randomBytes).Replace('-', '').ToLowerInvariant()
            $passwordFile = Join-Path $localDir 'initdb-password.txt'
            [System.IO.File]::WriteAllText($passwordFile, $password, [System.Text.UTF8Encoding]::new($false))
            try {
                & (Join-Path $postgresBin 'initdb.exe') -D $dataDir -U travel_planner_dev --pwfile=$passwordFile --auth=scram-sha-256 --encoding=UTF8 --locale=C
                if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL initialization failed.' }
            } finally {
                Remove-Item -LiteralPath $passwordFile -ErrorAction SilentlyContinue
            }
            $settings = "PORT=8080`nDATABASE_URL=postgres://travel_planner_dev:${password}@127.0.0.1:55432/travel_planner?sslmode=disable`nTEST_DATABASE_URL=postgres://travel_planner_dev:${password}@127.0.0.1:55432/travel_planner_test?sslmode=disable`n"
            [System.IO.File]::WriteAllText($envFile, $settings, [System.Text.UTF8Encoding]::new($false))
        }
        Start-LocalDatabase
        $connectionLine = [System.IO.File]::ReadAllLines($envFile) | Where-Object { $_.StartsWith('DATABASE_URL=') } | Select-Object -First 1
        $connection = [uri]$connectionLine.Substring('DATABASE_URL='.Length)
        $password = [uri]::UnescapeDataString(($connection.UserInfo -split ':', 2)[1])
        $previousPassword = $env:PGPASSWORD
        $env:PGPASSWORD = $password
        try {
            foreach ($databaseName in @('travel_planner', 'travel_planner_test')) {
                $exists = & (Join-Path $postgresBin 'psql.exe') -w -h 127.0.0.1 -p 55432 -U travel_planner_dev -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$databaseName'"
                if ($LASTEXITCODE -ne 0) { throw 'Could not check local databases.' }
                if ($exists -eq '1') { continue }
                & (Join-Path $postgresBin 'createdb.exe') -h 127.0.0.1 -p 55432 -U travel_planner_dev $databaseName
                if ($LASTEXITCODE -ne 0) { throw "Could not create $databaseName." }
            }
        } finally {
            $env:PGPASSWORD = $previousPassword
        }
        Write-Output 'Local development and test databases are ready on port 55432. Connection settings are saved in the ignored .env file.'
    }
    'start' { Start-LocalDatabase }
    'stop' {
        & $pgCtl -D $dataDir -m fast -w stop
        if ($LASTEXITCODE -ne 0) { throw 'Could not stop project PostgreSQL.' }
    }
    'status' {
        & $pgCtl -D $dataDir status
        exit $LASTEXITCODE
    }
}
