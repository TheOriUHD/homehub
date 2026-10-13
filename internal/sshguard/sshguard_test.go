package sshguard

import "testing"

func TestBlocked(t *testing.T) {
	cases := []string{
		"rm -rf /",
		"rm -rf /*",
		"sudo rm -rf --no-preserve-root /",
		"rm -fr /var",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda bs=1M",
		"wipefs -a /dev/sdb",
		"shred /dev/sda",
		"echo hi > /dev/sda",
		":(){ :|:& };:",
		"sgdisk --zap-all /dev/sda",
		"parted /dev/sda mklabel gpt",
		"userdel -r root",
		"echo x > /etc/passwd",
		"zpool destroy tank",
		"zfs destroy -r tank/data",
		"history -c",
		"iptables -F",
		"ls -la && rm -rf /", // dangerous suffix behind a safe prefix
		"cat /etc/hosts; mkfs.ext4 /dev/sdb1",
	}
	for _, c := range cases {
		if v := Classify(c); v.Risk != RiskBlocked {
			t.Errorf("Classify(%q) = %s (%s), want blocked", c, v.Risk, v.Reason)
		}
	}
}

func TestSafe(t *testing.T) {
	cases := []string{
		"ls -la /var/log",
		"cat /etc/os-release",
		"df -h",
		"free -m",
		"uptime",
		"ps aux",
		"systemctl status nginx",
		"systemctl is-active docker",
		"journalctl -u homehub -n 50",
		"docker ps -a",
		"docker logs mycontainer",
		"ip a",
		"ss -tulpn",
		"grep -r pattern /etc",
		"tail -n 100 /var/log/syslog",
		"sudo journalctl -u ssh",       // sudo'd read stays safe
		"cat /proc/meminfo | grep Mem", // pipeline of safe commands
		"qm list",
		"zpool status",
		"lsblk",
		"uname -a",
	}
	for _, c := range cases {
		if v := Classify(c); v.Risk != RiskSafe {
			t.Errorf("Classify(%q) = %s (%s), want safe", c, v.Risk, v.Reason)
		}
	}
}

func TestConfirm(t *testing.T) {
	cases := []string{
		"systemctl restart nginx",
		"apt-get install -y htop",
		"docker restart web",
		"reboot",
		"shutdown -h now",
		"rm /tmp/one-file",
		"mv /etc/a /etc/b",
		"chown -R me /srv",
		"echo hello > /tmp/file", // any write redirect
		"sed -i s/a/b/ /etc/conf",
		"kill -9 1234",
		"some-unknown-binary --flag", // unknown ⇒ fail closed
		"ls -la && systemctl stop nginx",
		"docker compose up -d",
		"truncate -s 0 /var/log/syslog",
	}
	for _, c := range cases {
		if v := Classify(c); v.Risk != RiskConfirm {
			t.Errorf("Classify(%q) = %s (%s), want confirm", c, v.Risk, v.Reason)
		}
	}
}

func TestEmpty(t *testing.T) {
	if v := Classify("   "); v.Risk != RiskConfirm {
		t.Errorf("empty command should not be safe, got %s", v.Risk)
	}
}
