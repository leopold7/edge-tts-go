package edgetts

import (
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

// Sec-MS-GEC token generation.
//
// Patched: this file has no upstream counterpart. Since October 2026
// Microsoft only accepts the v2 Sec-MS-GEC token: a "<16 hex CS64>-<64 hex
// SHA-256>" string whose digests are keyed by the leading bytes of the hashed
// input. The v1 token (a plain SHA-256 hex digest) is rejected with HTTP 403
// during the WebSocket handshake, which is what makes the Read Aloud endpoint
// fail worldwide. See edge-tts 7.3.0/7.3.1 and rany2/edge-tts#492.
//
// The helpers below are a literal port of `generate_sec_ms_gec_v2` and its
// helpers from edge-tts `drm.py`, down to the counters and the midstate
// adjustments: they are not a general SHA-256 and must not be reused for
// anything else.

const (
	// winEpochSeconds is the distance between the Windows file time epoch
	// (1601-01-01) and the Unix epoch.
	winEpochSeconds = 11644473600

	// secMSGECWindow is the length in seconds of the window a token is valid
	// for: the timestamp is rounded down to it, so every request inside the
	// same window carries the same token.
	secMSGECWindow = 300

	secSHA256Adjustment uint32 = 0xC9DC06A7
	secSigma0Adjustment uint32 = 0x059C0B80
	secSigma1Adjustment uint32 = 0x000B92C0

	secCS64Factor      uint32 = 0x82893933
	secCS64Adjustment0 uint32 = 0xF2F90B1A
	secCS64Adjustment1 uint32 = 0x0B1A0000
)

// FIPS 180-4 SHA-256 constants.
var (
	secSHA256Initial = [8]uint32{
		0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A, 0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
	}

	// secSHA256State and secSHA256Schedule are the midstate after the leading
	// salt bytes of the hashed input, so the rounds can resume at round 14
	// instead of hashing the prefix on every request.
	secSHA256State = [8]uint32{
		0xFD9FBEA2, 0x91243CE7, 0xBE499094, 0xCD2E1E0A, 0x6C006F49, 0x04BD35AB, 0xFEB0A7E4, 0x03EFFBD2,
	}

	secSHA256Schedule = [14]uint32{
		0xEB24BF9E, 0x1558691F, 0xA0C09EE2, 0x0CFA32BA, 0xA622EE8C, 0xD1DD9491, 0xC0DD25A0, 0xF9E70AB6,
		0xF0C31573, 0x7058CE4E, 0x7188E606, 0x00E5AB33, 0xD258B060, 0x72766564,
	}

	secSHA256RoundConstants = [64]uint32{
		0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5, 0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
		0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3, 0x72BE5D74, 0x80DEB1FE, 0x9BDC06A7, 0xC19BF174,
		0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC, 0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
		0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7, 0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
		0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13, 0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
		0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3, 0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
		0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5, 0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
		0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208, 0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
	}

	// secSHA256BlockConstants is secSHA256RoundConstants with the round 14
	// constant replaced by the midstate adjustment.
	secSHA256BlockConstants = [64]uint32{
		0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5, 0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
		0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3, 0x72BE5D74, 0x80DEB1FE, 0xC9DC06A7, 0xC19BF174,
		0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC, 0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
		0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7, 0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
		0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13, 0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
		0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3, 0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
		0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5, 0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
		0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208, 0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
	}

	// secCS64Coefficients are the (coefficient, offset) pairs the CS64 digest
	// mixes in before the input words.
	secCS64Coefficients = [14][2]uint32{
		{0xE82E91B1, 0xB32700D0},
		{0x44DE24C6, 0x88DFF51D},
		{0x3BEB65BC, 0x29514038},
		{0xC89C5933, 0xB328CBDF},
		{0x8D1EC211, 0xD405B4D5},
		{0x314EF5D5, 0x26E55F3C},
		{0xE92491B1, 0x607801C6},
		{0xF0A5B9D5, 0x82B2C1FB},
		{0x06064CF3, 0xFA51A3DA},
		{0xDA79FA60, 0x207CC464},
		{0xBED20560, 0x3C3C74F3},
		{0x89F9C9F8, 0xD2D14B61},
		{0x35FA38C6, 0xBB04A2C1},
		{0xCB49E3C6, 0xC5626167},
	}
)

// generateSecMSGEC builds the Sec-MS-GEC token for the five minute window that
// contains the current time, corrected by the clock skew learned from a
// rejected handshake.
func generateSecMSGEC() string {
	return generateSecMSGECv2(secMSGECTicks())
}

// generateSecMSGECv2 builds the v2 token from Windows file time ticks, which
// are 100ns intervals since 1601-01-01 already rounded down to secMSGECWindow.
func generateSecMSGECv2(ticks uint64) string {
	cs64, sha256 := secTokenHash([]byte(strconv.FormatUint(ticks, 10) + trustedClientToken))
	return cs64 + "-" + sha256
}

// secMSGECTicks returns the current time as Windows file time ticks, rounded
// down to the start of the current token window.
func secMSGECTicks() uint64 {
	clockSkewMu.Lock()
	skew := clockSkew
	clockSkewMu.Unlock()

	seconds := uint64(time.Now().Add(skew).Unix() + winEpochSeconds)
	seconds -= seconds % secMSGECWindow
	return seconds * ticksPerSecond
}

// secTokenHash returns the CS64 and SHA-256 digests of the token input as
// upper case hex.
func secTokenHash(data []byte) (string, string) {
	digest := secTokenSHA256(data)
	cs64 := secCS64Digest(data, digest)

	var cs64Bytes [8]byte
	binary.LittleEndian.PutUint32(cs64Bytes[:4], cs64[0])
	binary.LittleEndian.PutUint32(cs64Bytes[4:], cs64[1])
	return strings.ToUpper(hex.EncodeToString(cs64Bytes[:])), strings.ToUpper(hex.EncodeToString(digest[:]))
}

// secTokenSHA256 returns the salted digest of the token input. The input must
// carry the seven byte salt its midstate was precomputed for.
func secTokenSHA256(data []byte) [32]byte {
	if len(data) < 7 {
		panic("edgetts: Sec-MS-GEC input is shorter than its salt")
	}

	// The length spans the salt block the midstate already consumed.
	length := 57 + len(data)
	padded := make([]byte, 0, len(data)-7+72)
	padded = append(padded, data[7:]...)
	padded = append(padded, 0x80)
	padded = append(padded, make([]byte, ((55-length)%64+64)%64)...)
	var lengthBytes [8]byte
	binary.BigEndian.PutUint64(lengthBytes[:], uint64(length)*8)
	padded = append(padded, lengthBytes[:]...)

	state := secSHA256Midstate(data[:7])
	for offset := 0; offset < len(padded); offset += 64 {
		words := make([]uint32, 16, 64)
		for i := range words {
			words[i] = binary.BigEndian.Uint32(padded[offset+i*4 : offset+i*4+4])
		}
		working := secSHA256Rounds(state, secExpandSchedule(words, 16), 0)
		for i := range state {
			state[i] += working[i]
		}
	}

	var digest [32]byte
	for i, word := range state {
		binary.BigEndian.PutUint32(digest[i*4:], word)
	}
	return digest
}

// secSHA256Midstate resumes SHA-256 after the seven leading salt bytes of the
// token input.
func secSHA256Midstate(salt []byte) [8]uint32 {
	words := make([]uint32, 16, 64)
	words[14] = uint32(salt[0])<<16 | uint32(salt[1])<<8 | uint32(salt[2])
	words[15] = binary.BigEndian.Uint32(salt[3:7])

	for index := 16; index < 30; index++ {
		value := secSHA256Schedule[index-16]
		if index-2 >= 14 {
			sigma := secSigma1(words[index-2])
			if index == 16 {
				sigma ^= secSigma1Adjustment
			}
			value += sigma
		}
		if index-7 >= 14 {
			value += words[index-7]
		}
		if index-15 >= 14 {
			sigma := secSigma0(words[index-15])
			if index == 29 {
				sigma ^= secSigma0Adjustment
			}
			value += sigma
		}
		words = append(words, value)
	}

	words = append(words, secSigma1(words[28])+words[23]+secSigma0(words[15])+words[14]+
		secSHA256Adjustment-secSHA256RoundConstants[14])

	working := secSHA256Rounds(secSHA256State, secExpandSchedule(words, 31), 14)

	var state [8]uint32
	for i := range state {
		state[i] = secSHA256Initial[i] + working[i]
	}
	return state
}

// secSHA256Rounds runs the SHA-256 rounds, optionally resuming inside the first
// block.
func secSHA256Rounds(state [8]uint32, words []uint32, firstRound int) [8]uint32 {
	a, b, c, d, e, f, g, h := state[0], state[1], state[2], state[3], state[4], state[5], state[6], state[7]

	constants := secSHA256RoundConstants[:]
	if firstRound == 14 {
		constants = secSHA256BlockConstants[:]
	}

	index := firstRound
	// A resumed block ends on an eight round boundary, after which the
	// registers keep their roles and only the two updated ones need assigning.
	for aligned := min(64, (firstRound+7)/8*8); index < aligned; index++ {
		t1 := h + secBigSigma1(e) + secCh(e, f, g) + constants[index] + words[index]
		t2 := secBigSigma0(a) + secMaj(a, b, c)
		a, b, c, d, e, f, g, h = t1+t2, a, b, c, d+t1, e, f, g
	}

	for ; index < 64; index += 8 {
		t1 := h + secBigSigma1(e) + secCh(e, f, g) + constants[index] + words[index]
		d += t1
		h = t1 + secBigSigma0(a) + secMaj(a, b, c)

		t1 = g + secBigSigma1(d) + secCh(d, e, f) + constants[index+1] + words[index+1]
		c += t1
		g = t1 + secBigSigma0(h) + secMaj(h, a, b)

		t1 = f + secBigSigma1(c) + secCh(c, d, e) + constants[index+2] + words[index+2]
		b += t1
		f = t1 + secBigSigma0(g) + secMaj(g, h, a)

		t1 = e + secBigSigma1(b) + secCh(b, c, d) + constants[index+3] + words[index+3]
		a += t1
		e = t1 + secBigSigma0(f) + secMaj(f, g, h)

		t1 = d + secBigSigma1(a) + secCh(a, b, c) + constants[index+4] + words[index+4]
		h += t1
		d = t1 + secBigSigma0(e) + secMaj(e, f, g)

		t1 = c + secBigSigma1(h) + secCh(h, a, b) + constants[index+5] + words[index+5]
		g += t1
		c = t1 + secBigSigma0(d) + secMaj(d, e, f)

		t1 = b + secBigSigma1(g) + secCh(g, h, a) + constants[index+6] + words[index+6]
		f += t1
		b = t1 + secBigSigma0(c) + secMaj(c, d, e)

		t1 = a + secBigSigma1(f) + secCh(f, g, h) + constants[index+7] + words[index+7]
		e += t1
		a = t1 + secBigSigma0(b) + secMaj(b, c, d)
	}

	return [8]uint32{a, b, c, d, e, f, g, h}
}

// secExpandSchedule expands the message schedule from firstWord to 64 words.
func secExpandSchedule(words []uint32, firstWord int) []uint32 {
	for index := firstWord; index < 64; index++ {
		words = append(words, secSigma1(words[index-2])+words[index-7]+
			secSigma0(words[index-15])+words[index-16])
	}
	return words
}

// secCS64Digest returns the two CS64 words for the token input, keyed by the
// digest it produced.
func secCS64Digest(data []byte, digest [32]byte) [2]uint32 {
	keyA := binary.LittleEndian.Uint32(digest[:4]) | 1
	keyB := binary.LittleEndian.Uint32(digest[4:8]) | 1
	factors := [2]uint32{keyA * secCS64Factor, keyB * secCS64Factor}

	// One term per coefficient, then one per padded input word.
	words := secWordsLE4Even(append([]byte{0}, data...))
	terms := make([]uint32, 0, len(secCS64Coefficients)+len(words))
	offsets := make([]uint32, 0, cap(terms))
	for index, coefficient := range secCS64Coefficients {
		terms = append(terms, coefficient[0]*factors[index%2])
		offsets = append(offsets, coefficient[1])
	}
	for index, word := range words {
		key := keyA
		offset := secRol16(word) * 0xEF0569FB
		if index%2 == 1 {
			key = keyB
			offset = secRol16(word) * 0xC31713DB
		}
		term := word * key
		if index == 0 {
			term += secCS64Adjustment0 * factors[0]
			offset += secCS64Adjustment1
		}
		terms = append(terms, term)
		offsets = append(offsets, offset)
	}

	var swapAcc, swapLast, state, acc uint32
	for index := 0; index < len(terms); index += 2 {
		x, y := terms[index], terms[index+1]

		t := secRol16(offsets[index] + x)
		q := t * 0x76F50F34
		swapAcc += secRol16(q)*0xC3EFEA97 + q

		t = secRol16(offsets[index+1] + y)
		q = t * 0x1881CE3C
		swapLast = secRol16(q)*0x35BD1EC9 + q
		swapAcc += swapLast

		state = state*keyA + x
		state = secRol16(state) * 0xCF98B111
		state = secRol16(state) * 0x87085B9F
		state = secRol16(state) * 0x12CEB96D
		state = secRol16(state) * 0x257E1D83
		acc += state

		t = state*keyB + y
		t = secRol16(t) * 0xA27416F5
		t = secRol16(t) * 0xD38396FF
		state = secRol16(secRol16(t)*0x7C932B89) * 0xBFA49F69
		acc += state
	}

	return [2]uint32{swapLast ^ state, swapAcc ^ acc}
}

// secWordsLE4Even splits data into little endian 32-bit words, zero padded to
// an even count.
func secWordsLE4Even(data []byte) []uint32 {
	padded := len(data) + (4-len(data)%4)%4
	words := make([]uint32, 0, padded/4+1)
	for offset := 0; offset < padded; offset += 4 {
		var chunk [4]byte
		copy(chunk[:], data[min(offset, len(data)):])
		words = append(words, binary.LittleEndian.Uint32(chunk[:]))
	}
	if len(words)%2 == 1 {
		words = append(words, 0)
	}
	return words
}

func secRol16(value uint32) uint32 { return bits.RotateLeft32(value, 16) }

func secSigma0(value uint32) uint32 {
	return (value >> 7) ^ (value << 25) ^ (value >> 18) ^ (value << 14) ^ (value >> 3)
}

func secSigma1(value uint32) uint32 {
	return (value >> 17) ^ (value << 15) ^ (value >> 19) ^ (value << 13) ^ (value >> 10)
}

func secCh(x, y, z uint32) uint32  { return z ^ (x & (y ^ z)) }
func secMaj(x, y, z uint32) uint32 { return (x & y) | (z & (x | y)) }

func secBigSigma0(value uint32) uint32 {
	return bits.RotateLeft32(value, -2) ^ bits.RotateLeft32(value, -13) ^ bits.RotateLeft32(value, -22)
}

func secBigSigma1(value uint32) uint32 {
	return bits.RotateLeft32(value, -6) ^ bits.RotateLeft32(value, -11) ^ bits.RotateLeft32(value, -25)
}
