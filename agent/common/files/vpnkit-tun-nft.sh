# Used by the additional TUN modules to share the existing split-routing sets.
: "${VK_IFACE:?}" "${VK_NET:?}" "${VK_PORT:?}" "${VK_CASCADE:?}"
split=""
if [ "$VK_CASCADE" = 1 ]; then
  split="table inet vpnkit_split {
    chain prerouting {
      iifname \"$VK_IFACE\" ip daddr @ru_nets meta mark set 0x1
      iifname \"$VK_IFACE\" ip daddr @ru_domains meta mark set 0x1
    }
  }"
fi
cat <<RULES | put "/etc/nftables.d/31-vpnkit-$VK_MODULE.nft" 644
table inet vpnkit_fw {
  chain input {
    udp dport $VK_PORT accept
    iifname "$VK_IFACE" ip saddr $VK_NET udp dport 53 accept
    iifname "$VK_IFACE" ip saddr $VK_NET tcp dport 53 accept
  }
  chain forward {
    iifname "$VK_IFACE" ip saddr $VK_NET accept
  }
}
$split
table ip vpnkit_$VK_MODULE {}
flush table ip vpnkit_$VK_MODULE
table ip vpnkit_$VK_MODULE {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr $VK_NET oifname != "$VK_IFACE" masquerade
  }
}
RULES
nft -c -f /etc/nftables.conf
systemctl reload nftables
