param([Parameter(Mandatory=$true)][string]$MsiPath,[Parameter(Mandatory=$true)][string]$OutputDir)
$ErrorActionPreference='Stop'
Add-Type @'
using System;
using System.IO;
using System.Runtime.InteropServices;
public static class PdfMsiBinary {
 [DllImport("msi.dll", CharSet=CharSet.Unicode)] static extern uint MsiOpenDatabase(string path, IntPtr mode, out uint db);
 [DllImport("msi.dll", CharSet=CharSet.Unicode)] static extern uint MsiDatabaseOpenView(uint db, string query, out uint view);
 [DllImport("msi.dll")] static extern uint MsiViewExecute(uint view, uint record);
 [DllImport("msi.dll")] static extern uint MsiViewFetch(uint view, out uint record);
 [DllImport("msi.dll")] static extern uint MsiRecordReadStream(uint record, uint field, byte[] data, ref uint size);
 [DllImport("msi.dll")] static extern uint MsiCloseHandle(uint handle);
 static void Check(uint code) { if(code!=0) throw new Exception("MSI error "+code); }
 public static void Extract(string path, string name, string target) {
  uint db=0,view=0,record=0;
  try {
   Check(MsiOpenDatabase(path,IntPtr.Zero,out db));
   Check(MsiDatabaseOpenView(db,"SELECT `Data` FROM `Binary` WHERE `Name`='"+name+"'",out view));
   Check(MsiViewExecute(view,0)); Check(MsiViewFetch(view,out record));
   using(var output=File.Create(target)) {
    byte[] buffer=new byte[65536];
    for(;;) { uint size=(uint)buffer.Length; Check(MsiRecordReadStream(record,1,buffer,ref size)); if(size==0) break; output.Write(buffer,0,(int)size); }
   }
  } finally { if(record!=0) MsiCloseHandle(record); if(view!=0) MsiCloseHandle(view); if(db!=0) MsiCloseHandle(db); }
 }
}
'@
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
foreach($arch in @('x86','x64')) {
    $name="Windows61_KB2999226_${arch}msu"
    $output=Join-Path $OutputDir "Windows6.1-KB2999226-$arch.msu"
    [PdfMsiBinary]::Extract((Resolve-Path -LiteralPath $MsiPath).Path,$name,[IO.Path]::GetFullPath($output))
    Get-FileHash -Algorithm SHA256 -LiteralPath $output
}
