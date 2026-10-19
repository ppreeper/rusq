# bench.ps1 — run the rusq benchmarks on Windows and archive the result.
#
#     .\scripts\bench.ps1
#     .\scripts\bench.ps1 -Pattern 'BenchmarkLex|BenchmarkParse' -Count 5
#
# The archive lands in bench\<sha>-<utc>.txt, the same convention as
# scripts/bench.sh, so a run recorded on Windows can be diffed against another
# Windows run. scripts/bench-gate.sh does the threshold comparison; it is a bash
# script, so use WSL or Git Bash for that part.
param(
    [string]$Pattern = '.',
    [string]$Benchtime = '200ms',
    [int]$Count = 1
)

$ErrorActionPreference = 'Stop'

$sha = (git rev-parse --short HEAD)
$utc = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')

New-Item -ItemType Directory -Force -Path bench | Out-Null
$out = Join-Path 'bench' "$sha-$utc.txt"

go test -run '^$' -bench $Pattern -benchmem -benchtime $Benchtime -count $Count ./internal/... |
    Tee-Object -FilePath $out

if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Host "archived $out"

# Process start is dominated by the loader on Windows, so it is measured from
# the outside rather than with BenchmarkStartup.
$exe = Join-Path 'bin' 'rusq.exe'
if (Test-Path $exe) {
    $times = 1..10 | ForEach-Object {
        (Measure-Command { & $exe version | Out-Null }).TotalMilliseconds
    }
    $median = ($times | Sort-Object)[5]
    Write-Host ('rusq version median: {0:N1} ms' -f $median)
} else {
    Write-Host 'build bin/rusq.exe to measure process start (go build -o bin/rusq.exe ./cmd/rusq)'
}
