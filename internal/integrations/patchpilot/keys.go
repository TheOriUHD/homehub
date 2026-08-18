package patchpilot

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// KeyPair is the orchestrator's SSH identity for one PatchPilot fleet. The
// private key authenticates every run; the public key is installed into each
// host's authorized_keys during bootstrap.
type KeyPair struct {
	PrivatePEM string
	PublicKey  string // "ssh-ed25519 AAAA... " authorized_keys form
}

// GenerateKeyPair creates a fresh ed25519 SSH keypair in memory.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pemBlock, err := ssh.MarshalPrivateKey(priv, "homehub-patchpilot")
	if err != nil {
		return nil, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PrivatePEM: string(pem.EncodeToMemory(pemBlock)),
		PublicKey:  string(ssh.MarshalAuthorizedKey(sshPub)),
	}, nil
}

// PublicKeyFromPrivate recomputes the authorized_keys public line from a
// stored private key, so we never need to persist the public half separately.
func PublicKeyFromPrivate(privatePEM string) (string, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privatePEM))
	if err != nil {
		return "", fmt.Errorf("parsing stored key: %w", err)
	}
	return string(ssh.MarshalAuthorizedKey(signer.PublicKey())), nil
}
