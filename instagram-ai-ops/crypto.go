package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

func keyBytes() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY"))
	if raw == "" {
		pw := strings.TrimSpace(os.Getenv("APP_ACCESS_PASSWORD"))
		if pw == "" {
			return nil, errors.New("앱 비밀번호 설정이 필요합니다")
		}
		raw = "encryption:" + pw
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func sealBytes(plain []byte) (string, error) {
	key, err := keyBytes()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, plain, nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func openBytes(v string) ([]byte, error) {
	key, err := keyBytes()
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("저장된 연결 정보가 손상되었습니다")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

func sealAccount(a Account) (string, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return sealBytes(b)
}

func openAccount(v string) (Account, error) {
	b, err := openBytes(v)
	if err != nil {
		return Account{}, err
	}
	var a Account
	if err := json.Unmarshal(b, &a); err != nil {
		return Account{}, err
	}
	return a, nil
}
