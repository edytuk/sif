// Copyright (c) 2020, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the LICENSE.md file
// distributed with the sources of this project regarding your rights to use or distribute this
// software.

package integrity

import (
	"bytes"
	"crypto"
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/openpgp"
	pgperrors "golang.org/x/crypto/openpgp/errors"
	"golang.org/x/crypto/openpgp/packet"
)

type testType struct {
	One int
	Two int
}

func TestSignAndEncodeJSON(t *testing.T) {
	e := getTestEntity(t)

	// Fake an encrypted key.
	encryptedKey := *e.PrivateKey
	encryptedKey.Encrypted = true

	tests := []struct {
		name    string
		key     *packet.PrivateKey
		hash    crypto.Hash
		wantErr bool
	}{
		{name: "EncryptedKey", key: &encryptedKey, wantErr: true},
		{name: "DefaultHash", key: e.PrivateKey},
		{name: "SHA1", key: e.PrivateKey, hash: crypto.SHA1},
		{name: "SHA224", key: e.PrivateKey, hash: crypto.SHA224},
		{name: "SHA256", key: e.PrivateKey, hash: crypto.SHA256},
		{name: "SHA384", key: e.PrivateKey, hash: crypto.SHA384},
		{name: "SHA512", key: e.PrivateKey, hash: crypto.SHA512},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			b := bytes.Buffer{}

			config := packet.Config{
				DefaultHash: tt.hash,
				Time:        fixedTime,
			}

			err := signAndEncodeJSON(&b, testType{1, 2}, tt.key, &config)
			if got, want := err, tt.wantErr; (got != nil) != want {
				t.Fatalf("got error %v, wantErr %v", got, want)
			}

			if err == nil {
				if err := verifyGolden(t.Name(), &b); err != nil {
					t.Fatalf("failed to verify golden: %v", err)
				}
			}
		})
	}
}

func TestVerifyAndDecodeJSON(t *testing.T) {
	e := getTestEntity(t)

	testValue := testType{1, 2}

	// This is used to corrupt the plaintext.
	corrupter := strings.NewReplacer(`{"One":1,"Two":2}`, `{"One":2,"Two":4}`)

	tests := []struct {
		name       string
		hash       crypto.Hash
		el         openpgp.EntityList
		corrupter  *strings.Replacer
		output     interface{}
		wantErr    error
		wantEntity *openpgp.Entity
	}{
		{name: "ErrUnknownIssuer", el: openpgp.EntityList{}, wantErr: pgperrors.ErrUnknownIssuer},
		{name: "Corrupted", el: openpgp.EntityList{e}, corrupter: corrupter, wantEntity: e},
		{name: "VerifyOnly", el: openpgp.EntityList{e}, wantEntity: e},
		{name: "DefaultHash", el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
		{name: "SHA1", hash: crypto.SHA1, el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
		{name: "SHA224", hash: crypto.SHA224, el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
		{name: "SHA256", hash: crypto.SHA256, el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
		{name: "SHA384", hash: crypto.SHA384, el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
		{name: "SHA512", hash: crypto.SHA512, el: openpgp.EntityList{e}, output: &testType{}, wantEntity: e},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			b := bytes.Buffer{}

			config := packet.Config{
				DefaultHash: tt.hash,
			}
			err := signAndEncodeJSON(&b, testValue, e.PrivateKey, &config)
			if err != nil {
				t.Fatal(err)
			}

			// Introduce corruption, if applicable.
			if tt.corrupter != nil {
				s := b.String()
				b.Reset()
				if _, err := tt.corrupter.WriteString(&b, s); err != nil {
					t.Fatal(err)
				}
			}

			// Verify and decode.
			e, rest, err := verifyAndDecodeJSON(b.Bytes(), tt.output, tt.el)

			// Shouldn't be any trailing bytes.
			if n := len(rest); n != 0 {
				t.Errorf("%v trailing bytes", n)
			}

			// Verify the error (if any) is appropriate.
			if tt.corrupter == nil {
				if got, want := err, tt.wantErr; !errors.Is(got, want) {
					t.Fatalf("got error %v, want %v", got, want)
				}
			} else {
				if _, ok := err.(pgperrors.SignatureError); !ok {
					t.Fatalf("unexpected error type: %T", err)
				}
			}

			if err == nil {
				if tt.output != nil {
					if got, want := tt.output, &testValue; !reflect.DeepEqual(got, want) {
						t.Errorf("got value %v, want %v", got, want)
					}
				}

				if got, want := e, tt.wantEntity; !reflect.DeepEqual(got, want) {
					t.Errorf("got entity %+v, want %+v", got, want)
				}
			}
		})
	}
}
