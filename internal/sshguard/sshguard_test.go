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
		"ls -la && rm -rf /",           // dangerous suffix behind a safe prefix
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
		"echo hello > /tmp/file",       // any write redirect
		"sed -i s/a/b/ /etc/conf",
		"kill -9 1234",
		"some-unknown-binary --flag",   // unknown ⇒ fail closed
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

// Each of these once classified as safe — a read-only program name in front
// of a flag, a wrapper or a shell construct that acts. None may run
// unattended.
func TestBypassesNeedConfirmation(t *testing.T) {
	cases := []string{
		// readers with acting flags
		"find / -name x -delete",
		"find . -exec rm {} \\;",
		"find . -execdir sh -c id \\;",
		"find /tmp -ok rm {} ;",
		"find / -fprint /etc/cron.d/x",
		"find . -fls /tmp/out",
		"sort -o /etc/passwd x",
		"sort --output=/etc/passwd x",
		"sort -ro /etc/hosts x",
		"sort --compress-program=sh x",
		"date -s 2020-01-01",
		"date 010100002020",
		"hostname evil",
		"tree -o /etc/motd",
		"uniq in /etc/passwd",
		"journalctl --vacuum-size=1M",
		"journalctl --rotate",
		"dmesg -C",
		"ss -K dst 10.0.0.1",
		"rg --pre ./x pattern",
		"less +!reboot /etc/hosts",
		"mount /dev/sdb1 /mnt",
		"route add default gw 10.0.0.1",
		"ifconfig eth0 down",
		// interpreters and wrappers
		"awk 'BEGIN{system(\"reboot\")}'",
		"gawk '{print > \"/etc/x\"}' f",
		"sed -i s/a/b/ /etc/conf",
		"perl -e 'unlink \"/etc/x\"'",
		"python3 -c 'import os'",
		"env reboot",
		"env -i reboot",
		"xargs rm < list",
		"echo /etc | xargs rm -r",
		"eval reboot",
		"exec reboot",
		"sh -c reboot",
		"bash -c 'ls; reboot'",
		"nohup reboot",
		"timeout 5 reboot",
		"tee /etc/passwd",
		"dd if=/dev/zero of=/tmp/x",
		// network changes behind a reader's name
		"ip link set eth0 down",
		"ip addr add 10.0.0.2/24 dev eth0",
		"ip route del default",
		"ip netns exec x reboot",
		"ip -b /tmp/cmds",
		// shell constructs
		"echo $(reboot)",
		"echo `reboot`",
		"echo \"$(reboot)\"",
		"cat <(reboot)",
		"diff >(reboot) x",
		"ls & reboot",
		"ls |& reboot",
		"ls\nreboot",
		"(reboot)",
		"echo hi > /tmp/file",
		"echo hi >> ~/.bashrc",
		"echo hi >| /tmp/f",
		"ls &> /tmp/out",
		"cat x 1> out",
		"cat <> /etc/passwd",
		"cat <<EOF",
		"echo 'unterminated",
		// smuggled programs and environment
		"./ls",
		"/tmp/cat /etc/hosts",
		"$CMD",
		"l? -la",
		"LD_PRELOAD=/tmp/x.so ls",
		"PAGER=reboot git log",
		"GIT_EXTERNAL_DIFF=reboot git diff",
		"sudo -s ls",
		"sudo -i",
		"sudo -E ls",
		"find . ${IFS}-delete",
		"find . -de*",
		"sort $OPT x",
		// subcommand tricks
		"kubectl -n get delete pod x",
		"systemctl -H status reboot",
		"git -c core.pager=reboot log",
		"git --exec-path=/tmp log",
		"git branch evil",
		"git branch -D main",
		"git remote add x y",
		"git log --output=/etc/passwd",
		"git diff --ext-diff",
		"docker -H tcp://x rm y",
		"dpkg -l -i x.deb",
		"dpkg -i x.deb",
	}
	for _, c := range cases {
		if v := Classify(c); v.Risk == RiskSafe {
			t.Errorf("Classify(%q) = safe; want it held for confirmation", c)
		}
	}
	// rm -r -f with split flags is still refused outright.
	if v := Classify("rm -r -f /var"); v.Risk != RiskBlocked {
		t.Errorf("rm -r -f = %s, want blocked", v.Risk)
	}
}

// The tightening must not turn everyday reads into prompts: the harmless
// spellings of the same programs stay safe.
func TestHarmlessSpellingsStaySafe(t *testing.T) {
	cases := []string{
		"find /var/log -name '*.log' -mtime -1",
		"find . -type f -print",
		"sort -n -k2 file",
		"sort -u /etc/group",
		"env",
		"date",
		"date +%s",
		"date -d yesterday +%F",
		"hostname",
		"hostname -I",
		"ip a",
		"ip r",
		"ip -br link show",
		"ip addr show dev eth0",
		"ip route get 1.1.1.1",
		"ifconfig",
		"ifconfig eth0",
		"route -n",
		"mount",
		"uniq -c sorted.txt",
		"tree -L 2 /etc",
		"less /etc/hosts",
		"journalctl -u nginx --since today --no-pager",
		"dmesg -T",
		"ss -tlnp",
		"grep -E 'a|b' /etc/hosts",
		"grep 'x;y' file",
		"ls 2>/dev/null",
		"cat /etc/hosts > /dev/null 2>&1",
		"ls &>/dev/null",
		"cat < /etc/hosts",
		"echo $HOME",
		"ls *.conf",
		"/usr/bin/ls -la",
		"LC_ALL=C sort file",
		"sudo -n journalctl -u ssh",
		"git -C /srv/app status",
		"git --no-pager log -n 5",
		"git branch -a",
		"git remote -v",
		"kubectl -n kube-system get pods",
		"systemctl --no-pager status nginx",
		"dpkg -l nginx",
		"ps aux | grep nginx | head -5",
		"df -h && free -m || uptime",
	}
	for _, c := range cases {
		if v := Classify(c); v.Risk != RiskSafe {
			t.Errorf("Classify(%q) = %s (%s), want safe", c, v.Risk, v.Reason)
		}
	}
}
