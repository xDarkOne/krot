# relocate: переезд узла на новый IP после клонирования у хостера (команды relocate и vpnkit-reip).
# Параметры: VK_NODE, VK_ROLE, VK_DOMAIN, VK_PEER_NAME. Описание — docs/20-server-modules/relocate.md
: "${VK_NODE:?}" "${VK_ROLE:?}" "${VK_DOMAIN:=}" "${VK_PEER_NAME:=}"
for f in relocate vpnkit-reip; do [ -s "$VK_RUN/src/$f" ] || die "нет файла $f от пульта"; done
apt_install perl openssh-client iproute2
put /usr/local/sbin/vpnkit-reip 755 < "$VK_RUN/src/vpnkit-reip"
put /usr/local/sbin/relocate 755 < "$VK_RUN/src/relocate"
mkdir -p /etc/vpnkit; chmod 700 /etc/vpnkit
printf 'NODE=%s\nROLE=%s\nDOMAIN=%s\nPEER_NAME=%s\n' "$VK_NODE" "$VK_ROLE" "$VK_DOMAIN" "$VK_PEER_NAME" | put /etc/vpnkit/relocate.conf 600
# удобное имя: relocate-msk, relocate-ams и т. п. (по имени узла из inventory)
safe=$(tr -c 'A-Za-z0-9._-' '_' <<<"$VK_NODE")
if [ "$safe" != relocate ]; then
  [ -e "/usr/local/sbin/relocate-$safe" ] || echo "/usr/local/sbin/relocate-$safe" >> "$VK_RUN/created.list"
  ln -sf /usr/local/sbin/relocate "/usr/local/sbin/relocate-$safe"
fi
say "переезд на новый IP: команда relocate-$safe (или relocate)"
