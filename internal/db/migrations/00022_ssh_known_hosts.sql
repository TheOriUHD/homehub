-- +goose Up
-- Trust-on-first-use host-key store shared by the interactive SSH bridges
-- (Proxmox guest terminal, remote Docker daemon, Minecraft RCON-over-SSH).
-- PatchPilot pins per managed-host row instead; these bridges have no such row,
-- so they pin here keyed by (host, port). Empty until first contact records the
-- fingerprint; every later connection is verified against it, closing the same
-- LAN man-in-the-middle window PatchPilot's pinning closes.
CREATE TABLE ssh_known_hosts (
	host        TEXT        NOT NULL,
	port        INT         NOT NULL,
	fingerprint TEXT        NOT NULL,
	first_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (host, port)
);

-- +goose Down
DROP TABLE ssh_known_hosts;
