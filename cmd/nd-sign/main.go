// nd-sign creates the update signing key and signs release files.
//
//	go run ./cmd/nd-sign keygen                     -> %USERPROFILE%\.nexdesk\update.key (+ prints public key)
//	go run ./cmd/nd-sign sign -version 1.3.0 dist\NexDesk.exe   -> dist\NexDesk.exe.sig
//
// Keep update.key private and backed up: without it no further update can be
// installed by existing clients. Never commit it.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eirahoutmoss/remotesupport/shared/update"
)

func defaultKey() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nexdesk", "update.key")
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		fs := flag.NewFlagSet("keygen", flag.ExitOnError)
		key := fs.String("key", defaultKey(), "özel anahtar dosyası")
		out := fs.String("pub", "", "genel anahtarın yazılacağı dosya (ör. shared/update/update-pubkey.txt)")
		fs.Parse(os.Args[2:])
		check(keygen(*key, *out))
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		key := fs.String("key", defaultKey(), "özel anahtar dosyası")
		ver := fs.String("version", "", "sürüm (ör. 1.3.0)")
		fs.Parse(os.Args[2:])
		if *ver == "" || fs.NArg() != 1 {
			usage()
		}
		check(sign(*key, *ver, fs.Arg(0)))
	default:
		usage()
	}
}

func keygen(keyPath, pubPath string) error {
	if _, err := os.Stat(keyPath); err == nil {
		return fmt.Errorf("%s zaten var — üzerine yazılmadı (eski istemciler bu anahtara güveniyor)", keyPath)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0600); err != nil {
		return err
	}
	p := base64.StdEncoding.EncodeToString(pub)
	if pubPath != "" {
		if err := os.WriteFile(pubPath, []byte(p+"\n"), 0644); err != nil {
			return err
		}
	}
	fmt.Println("Özel anahtar:", keyPath, "(YEDEKLEYİN, paylaşmayın)")
	fmt.Println("Genel anahtar:", p)
	return nil
}

func sign(keyPath, version, file string) error {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("anahtar okunamadı: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("anahtar dosyası bozuk")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := update.Sign(ed25519.NewKeyFromSeed(seed), filepath.Base(file), version, data)
	if err := os.WriteFile(file+".sig", []byte(sig+"\n"), 0644); err != nil {
		return err
	}
	fmt.Println("İmzalandı:", file+".sig")
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "kullanım: nd-sign keygen [-key yol] [-pub dosya] | nd-sign sign -version X.Y.Z [-key yol] dosya")
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "HATA:", err)
		os.Exit(1)
	}
}
