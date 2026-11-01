package main

// Linux file-system encryption (fscrypt, policy v2) for a user's folder on
// orOS USB (vault.go). Only what the vault needs: add a key to the file
// system (the folder's files become readable), set the policy on an empty
// folder, ask for the key's status and remove it again (the files are
// locked: names and contents unreadable until the key is added again).
// Non-root users may do all of it for keys they added (kernel 5.4+).

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const fsKeyLen = 64 // FSCRYPT_MAX_KEY_SIZE: AES-256-XTS needs 64 bytes

type fsKeyID [unix.FSCRYPT_KEY_IDENTIFIER_SIZE]byte

func fsIoctl(dir string, req uintptr, arg unsafe.Pointer) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _, e := unix.Syscall(unix.SYS_IOCTL, f.Fd(), req, uintptr(arg))
	if e != 0 {
		return e
	}
	return nil
}

// fsAddKey adds a raw key to the file system that holds dir and returns
// the identifier the kernel derived from it.
func fsAddKey(dir string, key []byte) (fsKeyID, error) {
	var id fsKeyID
	if len(key) != fsKeyLen {
		return id, errors.New("fscrypt: bad key length")
	}
	hdr := int(unsafe.Sizeof(unix.FscryptAddKeyArg{}))
	buf := make([]byte, hdr+len(key))
	arg := (*unix.FscryptAddKeyArg)(unsafe.Pointer(&buf[0]))
	arg.Key_spec.Type = unix.FSCRYPT_KEY_SPEC_TYPE_IDENTIFIER
	arg.Raw_size = uint32(len(key))
	copy(buf[hdr:], key)
	err := fsIoctl(dir, unix.FS_IOC_ADD_ENCRYPTION_KEY, unsafe.Pointer(&buf[0]))
	copy(id[:], arg.Key_spec.U[:len(id)])
	for i := hdr; i < len(buf); i++ {
		buf[i] = 0
	}
	return id, err
}

// fsSetPolicy encrypts the (empty) folder dir, and so everything later
// made in it, with the key id.
func fsSetPolicy(dir string, id fsKeyID) error {
	p := unix.FscryptPolicyV2{
		Version:                   unix.FSCRYPT_POLICY_V2,
		Contents_encryption_mode:  unix.FSCRYPT_MODE_AES_256_XTS,
		Filenames_encryption_mode: unix.FSCRYPT_MODE_AES_256_CTS,
		Flags:                     unix.FSCRYPT_POLICY_FLAGS_PAD_32,
		Master_key_identifier:     id,
	}
	return fsIoctl(dir, unix.FS_IOC_SET_ENCRYPTION_POLICY, unsafe.Pointer(&p))
}

// fsPolicyKey returns the key id of the folder's policy (ok false: the
// folder is not encrypted).
func fsPolicyKey(dir string) (fsKeyID, bool) {
	var id fsKeyID
	arg := unix.FscryptGetPolicyExArg{Size: uint64(len(unix.FscryptGetPolicyExArg{}.Policy))}
	if fsIoctl(dir, unix.FS_IOC_GET_ENCRYPTION_POLICY_EX, unsafe.Pointer(&arg)) != nil {
		return id, false
	}
	p := (*unix.FscryptPolicyV2)(unsafe.Pointer(&arg.Policy[0]))
	if p.Version != unix.FSCRYPT_POLICY_V2 {
		return id, false
	}
	return p.Master_key_identifier, true
}

func fsSpec(id fsKeyID) unix.FscryptKeySpecifier {
	s := unix.FscryptKeySpecifier{Type: unix.FSCRYPT_KEY_SPEC_TYPE_IDENTIFIER}
	copy(s.U[:], id[:])
	return s
}

// fsKeyPresent: the key is in the file system that holds fs (the folder
// is open).
func fsKeyPresent(fs string, id fsKeyID) bool {
	arg := unix.FscryptGetKeyStatusArg{Key_spec: fsSpec(id)}
	if fsIoctl(fs, unix.FS_IOC_GET_ENCRYPTION_KEY_STATUS, unsafe.Pointer(&arg)) != nil {
		return false
	}
	return arg.Status == unix.FSCRYPT_KEY_STATUS_PRESENT
}

// fsRemoveKey locks the folder again. fs is any path on the same file
// system OUTSIDE the encrypted folder: an open handle on the folder itself
// keeps it busy. busy: some file was still open, so
// the kernel keeps it until that file closes (call again later).
func fsRemoveKey(fs string, id fsKeyID) (busy bool, err error) {
	arg := unix.FscryptRemoveKeyArg{Key_spec: fsSpec(id)}
	if err := fsIoctl(fs, unix.FS_IOC_REMOVE_ENCRYPTION_KEY, unsafe.Pointer(&arg)); err != nil {
		if errors.Is(err, unix.ENOKEY) {
			return false, nil
		}
		return false, err
	}
	return arg.Removal_status_flags&unix.FSCRYPT_KEY_REMOVAL_STATUS_FLAG_FILES_BUSY != 0, nil
}
