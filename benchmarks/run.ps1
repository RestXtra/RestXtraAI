param(
  [string]$OutFile = "",
  [int]$Count = 5,
  [string]$Benchtime = "1s"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$cache = Join-Path $repo ".gocache-bench"
$env:GOCACHE = $cache

try {
  $args = @(
    "test", "-run", "^$", "-bench", "Benchmark",
    "-benchmem", "-count", $Count, "-benchtime", $Benchtime,
    "./server", "./db", "./agent"
  )
  if ($OutFile) {
    $target = if ([IO.Path]::IsPathRooted($OutFile)) { $OutFile } else { Join-Path $repo $OutFile }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
    & go @args | Tee-Object -FilePath $target
  } else {
    & go @args
  }
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
  if (Test-Path -LiteralPath $cache) { Remove-Item -LiteralPath $cache -Recurse -Force }
}
