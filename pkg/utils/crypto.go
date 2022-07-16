package utils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
)

// Hash returns the hash of the inputted
// bytes as a hex string
// Inputs:
// v	[]byte	the value to be hashed
// Returns:
// string	the hash of the value represented
// as a hex string
func Hash(v []byte) string {
	h := sha256.Sum256(v)
	return hex.EncodeToString(h[:])
}

// Sign signs a message (a hash) using a
// private key and returns the signature.
// Inputs:
// sk *ecdsa.PrivateKey the private key
// h []byte the hash to be signed
// Returns:
// string	the signature represented as a
// hex string
// error	any error that happened with
// the signing process
func Sign(sk *ecdsa.PrivateKey, h []byte) ([]byte, error) {
	sigB, err := ecdsa.SignASN1(rand.Reader, sk, h)
	return sigB, err
}

// Verify verifies that a hash was signed using a public
// key. It returns an error if the signature is not valid
func Verify(pk *ecdsa.PublicKey, hash string, sig []byte) bool {
	if ok := ecdsa.VerifyASN1(pk, []byte(hash), sig); !ok {
		return false
	}
	return true
}

// Byt2PK deserializes the bytes
// to reconstruct a public key.
// Inputs:
// pkB []byte the public key in
// bytes
// Returns:
// *ecdsa.PublicKey the deserialized
// public key from the inputted bytes
// error	any error that happened with
// the deserializing the bytes to a public
// key
func Byt2PK(pkB []byte) (*ecdsa.PublicKey, error) {
	pk, err := x509.ParsePKIXPublicKey(pkB)
	if err != nil {
		return nil, err
	}
	return pk.(*ecdsa.PublicKey), nil
}

// PkFromSk converts the byte form of a secret key
// to the byte form of a public key
func PkFromSk(skB []byte) []byte {
	sc, _ := x509.ParseECPrivateKey(skB)
	pk := &(sc.PublicKey)
	pkB, _ := x509.MarshalPKIXPublicKey(pk)
	return pkB
}

// Byt2SK deserializes the bytes
// to reconstruct a private key.
// Inputs:
// skB []byte the private key in
// bytes
// Returns:
// *ecdsa.PrivateKey the deserialized
// private key from the inputted bytes
// error	any error that happened with
// the deserializing the bytes to a private
// key
func Byt2SK(skB []byte) (*ecdsa.PrivateKey, error) {
	return x509.ParseECPrivateKey(skB)
}
