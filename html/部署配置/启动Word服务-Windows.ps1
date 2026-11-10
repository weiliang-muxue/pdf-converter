# Compatibility entry: delegate to the automatic launcher in the html directory.
& (Join-Path (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)) 'start-windows.ps1') @args