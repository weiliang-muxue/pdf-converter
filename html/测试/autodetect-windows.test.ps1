param([string]$Launcher=(Join-Path (Split-Path -Parent $PSScriptRoot) 'start-windows.ps1'))
$ErrorActionPreference='Stop'
$oldArch=$env:PROCESSOR_ARCHITECTURE; $oldCompat=$env:PROCESSOR_ARCHITEW6432
function Get-WmiObject { param($Class) return $global:pdfTestOS }
try {
    foreach($case in @(
        @('6.1.7601','AMD64',1,'Windows7'),@('6.1.7601','x86',1,'Windows7'),
        @('10.0.19045','x86',0,'Windows7'),@('10.0.19045','AMD64',0,'Windows10'),
        @('10.0.26100','AMD64',0,'Windows10')
    )) {
        $global:pdfTestOS=New-Object PSObject -Property @{Version=$case[0];Caption='Mock Windows';ServicePackMajorVersion=$case[2]}
        $env:PROCESSOR_ARCHITEW6432='';$env:PROCESSOR_ARCHITECTURE=$case[1]
        $output=& $Launcher -CheckOnly | Out-String
        if($output -notmatch ($case[3]+' 离线版')){throw $output}
        Write-Output ('PASS: dry-run '+$case[0]+' / '+$case[1]+' -> '+$case[3])
    }
    foreach($case in @(@('6.1.7600','AMD64',0),@('10.0.26100','ARM64',0),@('5.1.2600','x86',3))) {
        $global:pdfTestOS=New-Object PSObject -Property @{Version=$case[0];Caption='Mock Windows';ServicePackMajorVersion=$case[2]}
        $env:PROCESSOR_ARCHITECTURE=$case[1]
        $failed=$false
        try { & $Launcher -CheckOnly | Out-Null } catch { $failed=$true }
        if(!$failed){throw 'Unsupported Windows accepted'}
    }
    Write-Output 'PASS: unsupported CPU, old OS and missing Win7 SP1 refused'
} finally { $env:PROCESSOR_ARCHITECTURE=$oldArch; $env:PROCESSOR_ARCHITEW6432=$oldCompat }
