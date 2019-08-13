package lastpass

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

func extractChunks(r io.Reader) ([]*chunk, error) {
	chunks := make([]*chunk, 0)
	for {
		chunkID, err := readID(r)
		if err != nil {
			if err == io.EOF {
				break
			}
		}
		payload, err := readItem(r)
		if err != nil {
			return nil, err
		}
		c := &chunk{chunkID, payload}
		chunks = append(chunks, c)
	}
	return chunks, nil
}

func readID(r io.Reader) (uint32, error) {
	var b [4]byte
	_, err := r.Read(b[:])
	if err != nil {
		return 0, err
	}
	return chunkIDFromBytes(b), nil
}

func readItem(r io.Reader) ([]byte, error) {
	size, err := readSize(r)
	if err != nil {
		return nil, err
	}
	b := make([]byte, size)
	n, err := r.Read(b)
	if err != nil {
		return nil, err
	}
	return b[:n], nil
}

func readSize(r io.Reader) (uint32, error) {
	var b [4]byte
	_, err := r.Read(b[:])
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func skipItem(r io.Reader) error {
	readSize, err := readSize(r)
	if err != nil {
		return err
	}
	b := make([]byte, readSize)
	_, err = r.Read(b)
	if err != nil {
		return err
	}
	return nil
}

func chunkIDFromBytes(b [4]byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func chunkIDFromString(s string) uint32 {
	b := []byte(s)
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func encryptAESCBC(plaintext string, encryptionKey []byte) (string, error) {
	if len(plaintext) == 0 {
		return "", nil
	}

	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	ciphertext := make([]byte, aes.BlockSize+len(padded))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	enc := cipher.NewCBCEncrypter(block, iv)
	enc.CryptBlocks(ciphertext[aes.BlockSize:], padded)

	ivBase64 := encodeBase64(iv)
	ciphertextBase64 := encodeBase64(ciphertext[aes.BlockSize:])

	// use the same format as the CLI does it in (v1.3.3)
	// https://github.com/lastpass/lastpass-cli/blob/a84aa9629957033082c5930968dda7fbed751dfa/cipher.c#L296
	return fmt.Sprintf("!%s|%s", ivBase64, ciphertextBase64), nil
}

func decryptItem(data []byte, encryptionKey []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	if data[0] != '!' {
		return "", errors.New("data is not AES 256 CBC enrypted: input doesn't start with '!'")
	}
	data = data[1:]

	var iv, in []byte
	if len(data)%16 == 0 {
		// CBC plain enrypted
		iv, in = data[:aes.BlockSize], data[aes.BlockSize:]

	} else {
		// CBC base 64 enrypted
		if data[24] != '|' {
			return "", errors.New("AES 256 CBC base64: can't determine length of IV")
		}

		ivBase64 := data[:24]
		var err error
		iv, err = decodeBase64(ivBase64)
		if err != nil {
			return "", err
		}

		inBase64 := data[25:]
		in, err = decodeBase64(inBase64)
		if err != nil {
			return "", err
		}
	}
	return decryptAES256CBC(iv, in, encryptionKey)
}

func decryptPrivateKey(privateKeyEncrypted string, encryptionKey []byte) (*rsa.PrivateKey, error) {
	privateKeyAESEncrypted, err := hex.DecodeString(privateKeyEncrypted)
	if err != nil {
		return nil, err
	}

	iv := encryptionKey[:aes.BlockSize]
	keyAnnotated, err := decryptAES256CBC(iv, privateKeyAESEncrypted, encryptionKey)
	if err != nil {
		return nil, err
	}

	keyTrimmed := strings.TrimPrefix(keyAnnotated, "LastPassPrivateKey<")
	keyTrimmed = strings.TrimSuffix(keyTrimmed, ">LastPassPrivateKey")

	keyPlain, err := hex.DecodeString(keyTrimmed)
	if err != nil {
		return nil, err
	}

	keyParsed, err := x509.ParsePKCS8PrivateKey(keyPlain)
	if err != nil {
		return nil, err
	}
	rsaPrivateKey, ok := keyParsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("did not find RSA private key type in PKCS#8 wrapping")
	}
	return rsaPrivateKey, nil
}

func decryptAES256CBC(iv, in, encryptionKey []byte) (string, error) {
	lenIn := len(in)
	if lenIn < aes.BlockSize {
		return "", fmt.Errorf("input is only %d bytes; expected at least %d bytes", lenIn, aes.BlockSize)
	}
	if lenIn%aes.BlockSize != 0 {
		return "", fmt.Errorf("input size (%d bytes) is not a multilpe of %d bytes", lenIn, aes.BlockSize)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	dec := cipher.NewCBCDecrypter(block, iv)
	out := make([]byte, lenIn)
	dec.CryptBlocks(out, in)
	return string(pkcs7Unpad(out)), nil
}

func encodeBase64(b []byte) []byte {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(encoded, b)
	return encoded
}

func decodeBase64(b []byte) ([]byte, error) {
	d := make([]byte, len(b))
	n, err := base64.StdEncoding.Decode(d, b)
	if err != nil {
		return nil, err
	}
	return d[:n], nil
}

func decodeHex(src []byte) ([]byte, error) {
	dst := make([]byte, hex.DecodedLen(len(src)))
	_, err := hex.Decode(dst, src)
	if err != nil {
		return nil, err
	}
	return dst, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

func pkcs7Unpad(data []byte) []byte {
	size := len(data)
	unpadding := int(data[size-1])
	return data[:(size - unpadding)]
}
