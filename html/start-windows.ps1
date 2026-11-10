param(
    [string]$Token = '',
    [string]$AppRoot = '',
    [ValidateRange(1024,65535)][int]$Port = 8787,
    [string]$SitePath = '',
    [string]$StateDir = '',
    [string]$NginxExe = '',
    [string]$NginxPrefix = '',
    [string]$NginxConfig = '',
    [switch]$CheckOnly
)
$ErrorActionPreference='Stop'
$htmlDir=Split-Path -Parent $MyInvocation.MyCommand.Path
if (!$AppRoot) { $AppRoot=Split-Path -Parent $htmlDir }
if (!$StateDir) { $StateDir=Join-Path $htmlDir '部署配置' }
# WMI reports the installed version even when the shell has an old Windows manifest.
$os=Get-WmiObject -Class Win32_OperatingSystem
$major=([Version]$os.Version).Major
$minor=([Version]$os.Version).Minor
$cpu=$env:PROCESSOR_ARCHITEW6432
if (!$cpu) { $cpu=$env:PROCESSOR_ARCHITECTURE }
if ($cpu -ne 'AMD64' -and $cpu -ne 'x86') { throw ('不支持的 Windows CPU 架构：'+$cpu) }
if ($major -lt 6 -or ($major -eq 6 -and $minor -lt 1)) { throw '至少需要 Windows 7 SP1。' }
if ($major -eq 6 -and $minor -eq 1 -and [int]$os.ServicePackMajorVersion -lt 1) { throw 'Windows 7 / Server 2008 R2 需要 SP1。' }
if ($major -ge 10 -and $cpu -eq 'AMD64') { $variant='Windows10'; $filename='PDF工具箱-Win10.exe' }
else { $variant='Windows7'; $filename='PDF工具箱-Win7.exe' }
$exe=Join-Path (Join-Path (Join-Path $AppRoot 'exe') $variant) $filename
if (!(Test-Path -LiteralPath $exe)) { throw ('缺少自动匹配的程序：'+$exe) }
Write-Output ('识别系统：'+$os.Caption+' '+$os.Version+' / '+$cpu)
Write-Output ('自动选择：'+$variant+' 离线版')
if ($CheckOnly) { Write-Output '只检查，不启动或修改配置。'; return }

New-Item -ItemType Directory -Force -Path $StateDir | Out-Null
$tokenFile=Join-Path $StateDir 'service-token.txt'
if (!$Token -and (Test-Path -LiteralPath $tokenFile)) { $Token=[IO.File]::ReadAllText($tokenFile).Trim() }
if (!$Token) { $Token=[Guid]::NewGuid().ToString('N')+[Guid]::NewGuid().ToString('N') }
if ($Token -notmatch '^[A-Za-z0-9_-]{24,128}$') { throw '口令只能包含 24 至 128 位英文字母、数字、下划线或短横线。' }
if ($SitePath -and $SitePath -notmatch '^/(?:[A-Za-z0-9_-]+/)*$') { throw 'SitePath 必须是 / 或 /pdf/ 这样的地址前缀。' }

function Test-PdfService {
    try {
        $request=[Net.HttpWebRequest]::Create("http://127.0.0.1:$Port/api/status")
        $request.Timeout=1500; $request.Proxy=$null; $request.Headers.Add('X-PDF-Token',$Token)
        $response=$request.GetResponse()
        try {
            $reader=New-Object IO.StreamReader($response.GetResponseStream())
            try { $body=$reader.ReadToEnd() } finally { $reader.Close() }
            return ($body -match '"version"' -and $body -match '"wordReady"')
        } finally { $response.Close() }
    } catch { return $false }
}
$alreadyRunning=Test-PdfService
if (!$alreadyRunning) {
    $probe=New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback,$Port)
    try { $probe.Start() } catch { throw "端口 $Port 已被占用，或已有服务使用了另一个口令。请先检查原服务。" } finally { $probe.Stop() }
}
$utf8=New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText($tokenFile,$Token,$utf8)
if (!$alreadyRunning) {
    $logDir=Join-Path $StateDir 'logs'
    New-Item -ItemType Directory -Force -Path $logDir | Out-Null
    $oldToken=$env:PDF_TOOL_TOKEN; $oldOS=$env:PDF_HOST_OS; $oldArch=$env:PDF_HOST_ARCH; $oldVariant=$env:PDF_ENGINE_VARIANT
    try {
        $env:PDF_TOOL_TOKEN=$Token; $env:PDF_HOST_OS=$os.Caption+' '+$os.Version
        $env:PDF_HOST_ARCH=$cpu; $env:PDF_ENGINE_VARIANT=$variant
        $process=Start-Process -FilePath $exe -ArgumentList @('--server','--listen',"127.0.0.1:$Port") -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logDir 'service.log') -RedirectStandardError (Join-Path $logDir 'error.log')
        [IO.File]::WriteAllText((Join-Path $StateDir 'service.pid'),[string]$process.Id,$utf8)
    } finally { $env:PDF_TOOL_TOKEN=$oldToken; $env:PDF_HOST_OS=$oldOS; $env:PDF_HOST_ARCH=$oldArch; $env:PDF_ENGINE_VARIANT=$oldVariant }
    $ready=$false
    for ($attempt=0; $attempt -lt 15; $attempt++) {
        Start-Sleep -Milliseconds 400
        if (Test-PdfService) { $ready=$true; break }
        if ($process.HasExited) { break }
    }
    if (!$ready) { throw ('服务未能启动，请查看 '+$logDir+' 中的日志。') }
    Write-Output ('服务启动成功，PID：'+$process.Id)
} else { Write-Output '已发现同口令服务，复用现有进程。' }
if (!$NginxExe) {
    $instances=@{}
    foreach($item in @(Get-WmiObject Win32_Process -Filter "Name='nginx.exe'")) {
        if (!$item.ExecutablePath) { continue }
        $binary=[IO.Path]::GetFullPath($item.ExecutablePath)
        $prefix=Split-Path -Parent $binary; $conf=''
        foreach($match in [regex]::Matches($item.CommandLine,'(?:^|\s)-(p|c)\s*(?:"([^"]+)"|(\S+))')) {
            $value=$match.Groups[2].Value; if(!$value){$value=$match.Groups[3].Value}
            if($match.Groups[1].Value -eq 'p'){$prefix=$value}else{$conf=$value}
        }
        $instances[$binary+'|'+$prefix+'|'+$conf]=@($binary,$prefix,$conf)
    }
    if($instances.Count -eq 1) {
        $instance=@($instances.Values)[0]; $NginxExe=$instance[0]
        if(!$NginxPrefix){$NginxPrefix=$instance[1]}; if(!$NginxConfig){$NginxConfig=$instance[2]}
    } elseif($instances.Count -gt 1) { throw '发现多个 Nginx 实例，为避免改错站点，请用 -NginxExe、-NginxPrefix、-NginxConfig 指定目标实例。' }
    else { throw '未找到正在运行的 Nginx。请确认已有 Nginx 正常运行，或用 -NginxExe 指定它的完整路径。' }
}
if(!$NginxPrefix){$NginxPrefix=Split-Path -Parent $NginxExe}
$setupArgs=@('--setup-nginx','--nginx',$NginxExe,'--nginx-prefix',$NginxPrefix,'--nginx-template',(Join-Path (Join-Path $htmlDir '部署配置') 'nginx-word.conf.template'),'--nginx-backups',(Join-Path $StateDir 'nginx-backups'),'--word-port',[string]$Port)
if($NginxConfig){$setupArgs+=@('--nginx-config',$NginxConfig)}
if($SitePath){$setupArgs+=@('--site-path',$SitePath)}
$oldToken=$env:PDF_TOOL_TOKEN
try { $env:PDF_TOOL_TOKEN=$Token; & $exe @setupArgs | ForEach-Object { Write-Output $_ }; if($LASTEXITCODE -ne 0){throw 'Nginx 自动配置未完成，请查看上面的具体原因。'} }
finally { $env:PDF_TOOL_TOKEN=$oldToken }
Write-Output '部署完成。用户打开原来的网页即可使用，不需要选择系统版本。'
