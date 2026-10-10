#!/bin/sh
# Writes deterministic API keys and auth-free settings for the lab services.
set -eu
cd "$(dirname "$0")/.."
mkdir -p state/prowlarr state/sonarr state/radarr state/qbittorrent/qBittorrent state/data/downloads state/data/media/tv state/data/media/movies state/torrents

arr() { # name port key
  [ -f "state/$1/config.xml" ] && return 0
  cat > "state/$1/config.xml" <<XML
<Config>
  <BindAddress>*</BindAddress>
  <Port>$2</Port>
  <SslPort>9898</SslPort>
  <EnableSsl>False</EnableSsl>
  <LaunchBrowser>False</LaunchBrowser>
  <ApiKey>$3</ApiKey>
  <AuthenticationMethod>External</AuthenticationMethod>
  <AuthenticationRequired>DisabledForLocalAddresses</AuthenticationRequired>
  <Branch>main</Branch>
  <LogLevel>info</LogLevel>
  <UrlBase></UrlBase>
  <UpdateMechanism>Docker</UpdateMechanism>
  <AnalyticsEnabled>False</AnalyticsEnabled>
</Config>
XML
}
arr prowlarr 9696 0123456789abcdef0123456789abcd03
arr sonarr 8989 0123456789abcdef0123456789abcd01
arr radarr 7878 0123456789abcdef0123456789abcd02

qbconf() {
mkdir -p "$1/qBittorrent"
[ -f "$1/qBittorrent/qBittorrent.conf" ] && return 0
cat > "$1/qBittorrent/qBittorrent.conf" <<'CONF'
[BitTorrent]
Session\AddExtensionToIncompleteFiles=true
Session\DefaultSavePath=/data/downloads
Session\DHTEnabled=false
Session\LSDEnabled=false
Session\PeXEnabled=false
Session\Port=6881
Session\QueueingSystemEnabled=false
Session\TempPathEnabled=false

[LegalNotice]
Accepted=true

[Preferences]
WebUI\Address=*
WebUI\AuthSubnetWhitelist=@Invalid()
WebUI\AuthSubnetWhitelistEnabled=false
WebUI\CSRFProtection=false
WebUI\HostHeaderValidation=false
WebUI\LocalHostAuth=false
WebUI\Password_PBKDF2="@ByteArray(ARQ77eY1NUZaQsuDHbIMCA==:0WMRkYTUWVT9wVvdDtHAjU9b3b7uB8NR1Gur2hmQCvCDpm39Q+PsJRJPaCU51dEiz+dTzh8qbPsL8WkFljQYFQ==)"
WebUI\Port=8080
WebUI\Username=admin
CONF
}
qbconf state/qbittorrent
for i in 1 2 3; do qbconf state/seed$i; done
echo seeded
