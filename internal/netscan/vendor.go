package netscan

import "strings"

// vendorOUI is a tiny built-in prefix→vendor map covering the hardware that
// actually shows up on a homelab. It is not the full IEEE registry — just
// enough to label the common cases; unknown prefixes return "".
var vendorOUI = map[string]string{
	"b8:27:eb": "Raspberry Pi",
	"dc:a6:32": "Raspberry Pi",
	"e4:5f:01": "Raspberry Pi",
	"28:cd:c1": "Raspberry Pi",
	"d8:3a:dd": "Raspberry Pi",
	"00:0c:29": "VMware",
	"00:50:56": "VMware",
	"00:1c:14": "VMware",
	"08:00:27": "VirtualBox",
	"52:54:00": "QEMU/KVM",
	"00:15:5d": "Hyper-V",
	"00:16:3e": "Xen",
	"fc:ec:da": "Ubiquiti",
	"74:83:c2": "Ubiquiti",
	"e0:63:da": "Ubiquiti",
	"18:e8:29": "Ubiquiti",
	"24:5a:4c": "Ubiquiti",
	"b4:fb:e4": "Ubiquiti",
	"00:1b:63": "Apple",
	"3c:15:c2": "Apple",
	"a4:83:e7": "Apple",
	"f0:18:98": "Apple",
	"ac:bc:32": "Apple",
	"dc:a9:04": "Apple",
	"00:1a:11": "Google",
	"f4:f5:d8": "Google",
	"da:a1:19": "Google",
	"00:e0:4c": "Realtek",
	"52:54:ab": "Realtek",
	"00:1d:0f": "TP-Link",
	"50:c7:bf": "TP-Link",
	"a4:2b:b0": "TP-Link",
	"00:11:32": "Synology",
	"00:90:a9": "Western Digital",
	"00:24:9b": "AVM (FRITZ!Box)",
	"08:96:d7": "AVM (FRITZ!Box)",
	"c8:0e:14": "AVM (FRITZ!Box)",
	"38:10:d5": "AVM (FRITZ!Box)",
	"e0:28:6d": "AVM (FRITZ!Box)",
	"00:17:88": "Philips Hue",
	"ec:b5:fa": "Philips Hue",
	"00:04:20": "Slim Devices",
	"b0:4e:26": "TP-Link",
	"18:fe:34": "Espressif (ESP)",
	"24:0a:c4": "Espressif (ESP)",
	"3c:71:bf": "Espressif (ESP)",
	"84:cc:a8": "Espressif (ESP)",
	"a0:20:a6": "Espressif (ESP)",
	"7c:9e:bd": "Espressif (ESP)",
	"bc:dd:c2": "Espressif (ESP)",
	"2c:f4:32": "Espressif (ESP)",
}

// vendorFor maps a MAC to a vendor via its 24-bit OUI prefix, or "".
func vendorFor(mac string) string {
	mac = strings.ToLower(mac)
	if len(mac) < 8 {
		return ""
	}
	return vendorOUI[mac[:8]]
}
