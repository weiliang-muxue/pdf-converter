param(
    [string]$LegacyGo = 'go',
    [string]$ModernGo = 'go',
    [string]$OutputDir = (Join-Path $PSScriptRoot '../exe')
)
$ErrorActionPreference = 'Stop'
$legacyVersion = & $LegacyGo version
if ($legacyVersion -notmatch 'go1\.20\.14 ') { throw 'Windows 7 兼容版必须使用 Go 1.20.14。通过 -LegacyGo 指定编译器路径。' }
$modernVersion = & $ModernGo version
if ($modernVersion -match 'go1\.20\.') { throw 'Linux 服务器版请使用受支持的现代 Go 编译器。' }
Push-Location $PSScriptRoot
$oldGOOS=$env:GOOS; $oldGOARCH=$env:GOARCH; $oldCGO=$env:CGO_ENABLED
try {
    $env:CGO_ENABLED='0'
    New-Item -ItemType Directory -Force -Path $OutputDir,(Join-Path $OutputDir 'Windows7'),(Join-Path $OutputDir 'Windows10'),(Join-Path $OutputDir '信创Linux') | Out-Null
    $env:GOOS='windows'; $env:GOARCH='386'
    & $LegacyGo build -mod=vendor -trimpath -ldflags '-s -w -H=windowsgui' -o (Join-Path $OutputDir 'Windows7/PDF工具箱-Win7.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Windows build failed' }
    $env:GOARCH='amd64'
    & $ModernGo build -mod=vendor -trimpath -ldflags '-s -w -H=windowsgui' -o (Join-Path $OutputDir 'Windows10/PDF工具箱-Win10.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'Windows 10 build failed' }
    $env:GOOS='linux'
    foreach($arch in @('amd64','arm64','loong64')) {
        $env:GOARCH=$arch
        & $ModernGo build -mod=vendor -trimpath -ldflags '-s -w' -o (Join-Path $OutputDir "信创Linux/pdf-tool-linux-$arch") .
        if ($LASTEXITCODE -ne 0) { throw "Linux $arch build failed" }
    }
    Get-ChildItem -LiteralPath $OutputDir -File -Recurse | Where-Object { $_.Name -like 'PDF工具箱-Win*.exe' -or $_.Name -like 'pdf-tool-linux-*' -or $_.Name -like 'runtime-*.tar.gz' -or $_.Extension -in '.msu','.sh' } | Sort-Object FullName | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $($_.Path.Substring((Resolve-Path $OutputDir).Path.Length+1).Replace('\','/'))" } | Set-Content -Encoding utf8 (Join-Path $OutputDir 'SHA256SUMS.txt')
} finally {
    $env:GOOS=$oldGOOS; $env:GOARCH=$oldGOARCH; $env:CGO_ENABLED=$oldCGO
    Pop-Location
}
