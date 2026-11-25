#ifndef AppVersion
  #define AppVersion "1.1.0"
#endif
#ifndef ArchSuffix
  #define ArchSuffix "x64"
#endif
#ifndef BinaryPath
  #define BinaryPath "..\..\dist\myovpn.exe"
#endif
#ifndef OpenVPNDir
  #define OpenVPNDir "..\..\build\openvpn-win"
#endif

[Setup]
AppId={{8D6D8F3B-3A14-4C93-B899-68482B19F1B0}
AppName=MyOVPN
AppVersion={#AppVersion}
AppPublisher=MyOVPN
DefaultDirName={autopf}\MyOVPN
DefaultGroupName=MyOVPN
DisableProgramGroupPage=yes
LicenseFile=..\..\LICENSE
OutputDir=..\..\dist
OutputBaseFilename=MyOVPN-Setup-{#AppVersion}-{#ArchSuffix}
SetupIconFile=..\..\assets\icons\myovpn.ico
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
#if ArchSuffix == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif

[Files]
Source: "{#BinaryPath}"; DestDir: "{app}"; DestName: "myovpn.exe"; Flags: ignoreversion
Source: "{#OpenVPNDir}\*"; DestDir: "{app}\openvpn"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\MyOVPN"; Filename: "{app}\myovpn.exe"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "MyOVPN"; ValueData: """{app}\myovpn.exe"""; Flags: uninsdeletevalue

[Run]
Filename: "{app}\myovpn.exe"; Parameters: "install-service"; Flags: runhidden waituntilterminated
Filename: "{app}\myovpn.exe"; Description: "Launch MyOVPN tray"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\myovpn.exe"; Parameters: "uninstall-service"; Flags: runhidden waituntilterminated
