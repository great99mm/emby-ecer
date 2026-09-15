param(
    [string]$SshAlias = 'netcup',
    [int]$LocalPort = 3000,
    [int]$RemotePort = 33000
)

# Run as a hidden process/startup shortcut so preview survives terminal shutdown.
$ErrorActionPreference = 'Continue'
$tunnelMutex = [System.Threading.Mutex]::new($false, "Local\EmbyEcerPreviewTunnel-$LocalPort")
if (-not $tunnelMutex.WaitOne(0)) { exit 0 }
try {
    while ($true) {
        & "$env:WINDIR\System32\OpenSSH\ssh.exe" -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -L "127.0.0.1:${LocalPort}:127.0.0.1:${RemotePort}" $SshAlias 2>> "$env:TEMP\embyecer-preview-tunnel.log"
        Start-Sleep -Seconds 3
    }
} finally {
    $tunnelMutex.ReleaseMutex()
    $tunnelMutex.Dispose()
}
