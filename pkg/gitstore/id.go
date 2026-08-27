package gitstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// Ids are ULIDs: a 48-bit millisecond timestamp then 80 random bits, in 26
// Crockford-base32 characters. The time comes first so ids sort in minting
// order; the tail is what the card layout shards on.

// crockfordAlphabet is the ULID alphabet (no I, L, O, U).
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ErrBadIDFormat is returned for a string that is not a ULID.
var ErrBadIDFormat = errors.New("gitstore: not a ulid")

// NewID mints an id for the given moment with random tail bits.
func NewID(t time.Time) string {
	var raw [16]byte
	putTime(&raw, t)
	if _, err := rand.Read(raw[6:]); err != nil {
		panic("gitstore: crypto/rand: " + err.Error())
	}
	return encodeULID(raw)
}

// DeriveID mints the id that (namespace, keys…) always maps to at the given
// moment: the tail is a hash of the inputs instead of random bits, so the
// migration gets byte-identical re-runs and two replicas filing the same
// iteration write the same path.
func DeriveID(t time.Time, namespace string, keys ...string) string {
	var raw [16]byte
	putTime(&raw, t)
	h := sha256.Sum256([]byte(namespace + "\x00" + strings.Join(keys, "\x00")))
	copy(raw[6:], h[:10])
	return encodeULID(raw)
}

// IDTime reads the moment an id was minted for.
func IDTime(id string) (time.Time, error) {
	raw, err := decodeULID(id)
	if err != nil {
		return time.Time{}, err
	}
	ms := int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 | int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
	return time.UnixMilli(ms).UTC(), nil
}

func putTime(raw *[16]byte, t time.Time) {
	ms := uint64(t.UnixMilli()) //nolint:gosec // dates after 1970 only
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], ms)
	copy(raw[:6], buf[2:]) // the low 48 bits, big-endian
}

// crockfordIndex maps a byte to its alphabet value, or 0xFF when it is not
// in the alphabet.
var crockfordIndex = func() [256]byte {
	var m [256]byte
	for i := range m {
		m[i] = 0xFF
	}
	for i := byte(0); i < 32; i++ {
		m[crockfordAlphabet[i]] = i
	}
	return m
}()

// encodeULID renders 128 bits as 26 base32 characters, the standard ULID
// layout (the first character carries only the top three bits).
func encodeULID(v [16]byte) string {
	e := crockfordAlphabet
	out := make([]byte, 26)
	out[0] = e[(v[0]&224)>>5]
	out[1] = e[v[0]&31]
	out[2] = e[(v[1]&248)>>3]
	out[3] = e[((v[1]&7)<<2)|((v[2]&192)>>6)]
	out[4] = e[(v[2]&62)>>1]
	out[5] = e[((v[2]&1)<<4)|((v[3]&240)>>4)]
	out[6] = e[((v[3]&15)<<1)|((v[4]&128)>>7)]
	out[7] = e[(v[4]&124)>>2]
	out[8] = e[((v[4]&3)<<3)|((v[5]&224)>>5)]
	out[9] = e[v[5]&31]
	out[10] = e[(v[6]&248)>>3]
	out[11] = e[((v[6]&7)<<2)|((v[7]&192)>>6)]
	out[12] = e[(v[7]&62)>>1]
	out[13] = e[((v[7]&1)<<4)|((v[8]&240)>>4)]
	out[14] = e[((v[8]&15)<<1)|((v[9]&128)>>7)]
	out[15] = e[(v[9]&124)>>2]
	out[16] = e[((v[9]&3)<<3)|((v[10]&224)>>5)]
	out[17] = e[v[10]&31]
	out[18] = e[(v[11]&248)>>3]
	out[19] = e[((v[11]&7)<<2)|((v[12]&192)>>6)]
	out[20] = e[(v[12]&62)>>1]
	out[21] = e[((v[12]&1)<<4)|((v[13]&240)>>4)]
	out[22] = e[((v[13]&15)<<1)|((v[14]&128)>>7)]
	out[23] = e[(v[14]&124)>>2]
	out[24] = e[((v[14]&3)<<3)|((v[15]&224)>>5)]
	out[25] = e[v[15]&31]
	return string(out)
}

// decodeULID reads 26 base32 characters back into 128 bits.
func decodeULID(s string) ([16]byte, error) {
	var v [16]byte
	if len(s) != 26 {
		return v, ErrBadIDFormat
	}
	d := make([]byte, 26)
	for i := 0; i < 26; i++ {
		d[i] = crockfordIndex[s[i]]
		if d[i] == 0xFF {
			return v, ErrBadIDFormat
		}
	}
	if d[0] > 7 {
		return v, ErrBadIDFormat // overflow: the first char holds three bits
	}
	v[0] = d[0]<<5 | d[1]
	v[1] = d[2]<<3 | d[3]>>2
	v[2] = d[3]<<6 | d[4]<<1 | d[5]>>4
	v[3] = d[5]<<4 | d[6]>>1
	v[4] = d[6]<<7 | d[7]<<2 | d[8]>>3
	v[5] = d[8]<<5 | d[9]
	v[6] = d[10]<<3 | d[11]>>2
	v[7] = d[11]<<6 | d[12]<<1 | d[13]>>4
	v[8] = d[13]<<4 | d[14]>>1
	v[9] = d[14]<<7 | d[15]<<2 | d[16]>>3
	v[10] = d[16]<<5 | d[17]
	v[11] = d[18]<<3 | d[19]>>2
	v[12] = d[19]<<6 | d[20]<<1 | d[21]>>4
	v[13] = d[21]<<4 | d[22]>>1
	v[14] = d[22]<<7 | d[23]<<2 | d[24]>>3
	v[15] = d[24]<<5 | d[25]
	return v, nil
}
