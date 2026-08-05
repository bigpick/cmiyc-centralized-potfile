// Package pgpcrypto builds the armored, encrypted submission payload entirely
// on the operator's machine. Encrypting to KoreLogic's public key needs no
// secret, but signing with the team key does, so keeping this client-side means
// no private key material ever reaches the hosted pool.
package pgpcrypto

import (
	"bytes"
	"fmt"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// EncryptArmored encrypts plaintext to the armored recipient public key and,
// if signerArmored is non-nil, signs it with that private key (unlocking it
// with passphrase when the key is protected). The result is an armored
// "PGP MESSAGE" ready to paste or attach.
func EncryptArmored(plaintext, recipientArmored, signerArmored, passphrase []byte) ([]byte, error) {
	recipients, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(recipientArmored))
	if err != nil {
		return nil, fmt.Errorf("read recipient key: %w", err)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("recipient key ring is empty")
	}

	var signer *openpgp.Entity
	if len(signerArmored) > 0 {
		signers, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(signerArmored))
		if err != nil {
			return nil, fmt.Errorf("read signing key: %w", err)
		}
		if len(signers) == 0 {
			return nil, fmt.Errorf("signing key ring is empty")
		}
		signer = signers[0]
		if err := unlock(signer, passphrase); err != nil {
			return nil, err
		}
	}

	var out bytes.Buffer
	armorWriter, err := armor.Encode(&out, "PGP MESSAGE", nil)
	if err != nil {
		return nil, fmt.Errorf("armor encode: %w", err)
	}

	ptWriter, err := openpgp.Encrypt(armorWriter, recipients, signer, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	if _, err := ptWriter.Write(plaintext); err != nil {
		return nil, fmt.Errorf("write plaintext: %w", err)
	}
	if err := ptWriter.Close(); err != nil {
		return nil, fmt.Errorf("finalize encryption: %w", err)
	}
	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("finalize armor: %w", err)
	}
	return out.Bytes(), nil
}

// unlock decrypts a protected private key and its subkeys in place.
func unlock(e *openpgp.Entity, passphrase []byte) error {
	if e.PrivateKey == nil {
		return fmt.Errorf("signing entity has no private key (is this a public key export?)")
	}
	if e.PrivateKey.Encrypted {
		if len(passphrase) == 0 {
			return fmt.Errorf("signing key is passphrase-protected but no passphrase was provided")
		}
		if err := e.PrivateKey.Decrypt(passphrase); err != nil {
			return fmt.Errorf("unlock signing key: %w", err)
		}
	}
	for _, sk := range e.Subkeys {
		if sk.PrivateKey != nil && sk.PrivateKey.Encrypted {
			_ = sk.PrivateKey.Decrypt(passphrase)
		}
	}
	return nil
}

// LoadKeyFile reads an armored key from disk. Returns nil, nil for an empty
// path so callers can treat "no signing key configured" uniformly.
func LoadKeyFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file %q: %w", path, err)
	}
	return b, nil
}
