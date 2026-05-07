$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$src = Join-Path $root 'src'
$out = Join-Path $root 'out'
$dist = Join-Path $root 'dist'
$jarFile = Join-Path $dist 'r0rpc-relay-client.jar'

if (Test-Path $out) {
    Remove-Item -Recurse -Force $out
}
New-Item -ItemType Directory -Force -Path $out | Out-Null
New-Item -ItemType Directory -Force -Path $dist | Out-Null

$javaFiles = Get-ChildItem -Path $src -Recurse -Filter *.java | Select-Object -ExpandProperty FullName
if (-not $javaFiles) {
    throw 'No Java source files found'
}

javac -encoding UTF-8 -source 8 -target 8 -d $out $javaFiles
jar --create --file $jarFile -C $out .
Write-Host "Built $jarFile"