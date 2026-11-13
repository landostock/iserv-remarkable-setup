; IServ → reMarkable per-user installer
; Required command-line defines: Arch, ArchAllowed, ArchInstallMode, Version,
; NumericVersion, BinDir, OutputDir. SetupIcon is optional.

#ifndef Arch
  #define Arch "amd64"
#endif
#ifndef ArchAllowed
  #define ArchAllowed "x64os"
#endif
#ifndef ArchInstallMode
  #define ArchInstallMode "x64os"
#endif
#ifndef Version
  #define Version "0.9.0-rc27"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.9.0.27"
#endif
#ifndef BinDir
  #define BinDir "."
#endif
#ifndef OutputDir
  #define OutputDir "."
#endif

[Setup]
AppId={{BE3B0E38-F429-4E57-B4E0-818921526385}
AppName=IServ → reMarkable
AppVerName=IServ → reMarkable {#Version}
AppVersion={#Version}
AppPublisher=IServ → reMarkable project
AppPublisherURL=https://github.com/landostock/iserv-remarkable-setup
AppSupportURL=https://github.com/landostock/iserv-remarkable-setup/issues
AppUpdatesURL=https://github.com/landostock/iserv-remarkable-setup/releases
DefaultDirName={localappdata}\Programs\IServ reMarkable Setup
DefaultGroupName=IServ → reMarkable
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed={#ArchAllowed}
ArchitecturesInstallIn64BitMode={#ArchInstallMode}
UninstallDisplayName=IServ → reMarkable
UninstallDisplayIcon={app}\app-icon.ico
OutputDir={#OutputDir}
OutputBaseFilename=IServ-reMarkable-windows-{#Arch}-Setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
VersionInfoVersion={#NumericVersion}
VersionInfoProductName=IServ → reMarkable
VersionInfoDescription=IServ → reMarkable setup and maintenance companion
VersionInfoCompany=IServ → reMarkable project
VersionInfoCopyright=Copyright © contributors
VersionInfoOriginalFileName=IServ-reMarkable-windows-{#Arch}-Setup.exe
#ifdef SetupIcon
SetupIconFile={#SetupIcon}
#endif

[Languages]
Name: "de"; MessagesFile: "compiler:Languages\German.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Files]
Source: "{#BinDir}\iserv-remarkable-setup.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\iserv-rm-folder-helper.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\app-icon.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\SECURITY.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\PRIVACY.md"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\IServ → reMarkable"; Filename: "{app}\iserv-remarkable-setup.exe"; IconFilename: "{app}\app-icon.ico"; WorkingDir: "{app}"

[Run]
Filename: "{app}\iserv-remarkable-setup.exe"; Description: "{cm:LaunchProgram,IServ → reMarkable}"; Flags: nowait postinstall skipifsilent
