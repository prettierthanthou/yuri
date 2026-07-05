// this is a small subset from solana-go, and is licensed under the MIT license.

// source: https://github.com/solana-foundation/solana-go/tree/main/base58
// copied license: [LICENSE](./LICENSE)
package solana

import (
	"crypto/sha256"
	"errors"

	"codeberg.org/lewdest/yuri/internal/solana/base58"
	"github.com/oasisprotocol/curve25519-voi/curve"
	voied25519 "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
)

const (
	MaxSeedLength = 32
	MaxSeeds      = 16
	PDA_MARKER    = "ProgramDerivedAddress"
)

var (
	TokenProgramID = mustPubkey("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
	ATAPROGRAMID   = mustPubkey("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
)

type PublicKey [32]byte

func mustPubkey(s string) PublicKey {
	b, err := base58.Decode(s)
	if err != nil || len(b) != 32 {
		panic("invalid pubkey: " + s)
	}
	var pk PublicKey
	copy(pk[:], b)
	return pk
}

func PublicKeyFromBase58(s string) (PublicKey, error) {
	b, err := base58.Decode(s)
	if err != nil {
		return PublicKey{}, err
	}
	if len(b) != 32 {
		return PublicKey{}, errors.New("invalid pubkey length")
	}
	var pk PublicKey
	copy(pk[:], b)
	return pk, nil
}

func (p PublicKey) String() string {
	return base58.Encode(p[:])
}

func (p PublicKey) Bytes() []byte {
	return p[:]
}

func AssociatedTokenAddress(owner, mint string) (string, error) {
	ownerPK, err := PublicKeyFromBase58(owner)
	if err != nil {
		return "", err
	}

	mintPK, err := PublicKeyFromBase58(mint)
	if err != nil {
		return "", err
	}

	ata, _, err := FindProgramAddress(
		[][]byte{
			ownerPK.Bytes(),
			TokenProgramID.Bytes(),
			mintPK.Bytes(),
		},
		ATAPROGRAMID,
	)
	if err != nil {
		return "", err
	}

	return ata.String(), nil
}

func FindProgramAddress(seeds [][]byte, programID PublicKey) (PublicKey, uint8, error) {
	for bump := uint8(255); ; bump-- {
		addr, err := CreateProgramAddress(
			append(seeds, []byte{bump}),
			programID,
		)
		if err == nil {
			return addr, bump, nil
		}

		if bump == 0 {
			break
		}
	}

	return PublicKey{}, 0, errors.New("unable to find valid program address")
}

func CreateProgramAddress(seeds [][]byte, programID PublicKey) (PublicKey, error) {
	if len(seeds) > MaxSeeds {
		return PublicKey{}, errors.New("too many seeds")
	}

	for _, s := range seeds {
		if len(s) > MaxSeedLength {
			return PublicKey{}, errors.New("seed too long")
		}
	}

	size := 0
	for _, s := range seeds {
		size += len(s)
	}

	size += len(programID)
	size += len(PDA_MARKER)

	buf := make([]byte, 0, size)

	for _, s := range seeds {
		buf = append(buf, s...)
	}

	buf = append(buf, programID[:]...)
	buf = append(buf, PDA_MARKER...)

	hash := sha256.Sum256(buf)

	// Must be OFF-curve for PDA validity
	if IsOnCurve(hash[:]) {
		return PublicKey{}, errors.New("invalid PDA: on-curve")
	}

	return PublicKeyFromBytes(hash[:]), nil
}

func PublicKeyFromBytes(b []byte) PublicKey {
	var pk PublicKey
	copy(pk[:], b)
	return pk
}

func IsOnCurve(b []byte) bool {
	if len(b) != voied25519.PublicKeySize {
		return false
	}

	var compressed curve.CompressedEdwardsY
	if _, err := compressed.SetBytes(b); err != nil {
		return false
	}

	var p curve.EdwardsPoint
	if _, err := p.SetCompressedY(&compressed); err != nil {
		return false
	}

	return true
}
