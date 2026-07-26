# Runs one Java comparison benchmark in a fresh JVM and reports peak process RSS.
# Usage: .\run.ps1 -Impl ours|yauaa|uap -N 100000 [-Cache 0]
param(
    [Parameter(Mandatory)][string]$Impl,
    [Parameter(Mandatory)][int]$N,
    [int]$Cache = 0
)

$cp = Join-Path $PSScriptRoot "target/classes"
$libs = Join-Path $PSScriptRoot "target/libs/*"
$corpus = Join-Path $PSScriptRoot "../corpus.json"
$out = Join-Path $PSScriptRoot "target/bench-out.txt"
$err = Join-Path $PSScriptRoot "target/bench-err.txt"

$p = Start-Process java `
    -ArgumentList '-Xmx2g', '-cp', "`"$cp;$libs`"", 'compare.Main', "`"$corpus`"", $Impl, $N, $Cache `
    -PassThru -NoNewWindow -RedirectStandardOutput $out -RedirectStandardError $err

$maxWS = 0
$lastWS = 0
while (-not $p.HasExited) {
    $p.Refresh()
    if ($p.WorkingSet64 -gt 0) { $lastWS = $p.WorkingSet64 }
    if ($p.WorkingSet64 -gt $maxWS) { $maxWS = $p.WorkingSet64 }
    Start-Sleep -Milliseconds 50
}

Get-Content $out
"peak RSS: {0:N1} MB, settled RSS: {1:N1} MB" -f ($maxWS / 1MB), ($lastWS / 1MB)
