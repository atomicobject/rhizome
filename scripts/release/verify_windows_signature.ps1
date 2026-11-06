param(
    [Parameter(Mandatory = $true)][string]$Path,
    [string]$ExpectedThumbprint = ""
)
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$signature = Get-AuthenticodeSignature -LiteralPath $Path
if ($signature.Status -ne "Valid" -or $null -eq $signature.SignerCertificate) {
    throw "Windows production artifact does not have a valid Authenticode signature."
}
$thumbprint = $signature.SignerCertificate.Thumbprint
if ($ExpectedThumbprint -and $thumbprint -ne $ExpectedThumbprint) {
    throw "Windows production artifact has an unexpected signing identity."
}
@{ status = "authenticode"; thumbprint = $thumbprint } | ConvertTo-Json -Compress
