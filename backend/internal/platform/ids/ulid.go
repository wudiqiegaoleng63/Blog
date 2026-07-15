// Package ids 提供对外标识符生成，例如 ULID 形式的 public_id。
//
// 不在首版引入第三方 ULID 库；使用标准库 crypto/rand 生成
// Crockford Base32 编码的 26 位时间排序标识符，满足博客对外 ID 需求。
package ids

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// ulidAlphabet 是 Crockford Base32 字母表（排除 I/L/O/U 以避免歧义）。
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ulidLen 是 ULID 的固定字符长度。
const ulidLen = 26

var errReadRandom = errors.New("ids: failed to read random bytes")

// NewULID 生成一个新的 26 位 ULID 字符串（大写）。
// 前 10 位编码 48-bit Unix 毫秒时间戳，后 16 位编码 80-bit 随机数。
func NewULID() (string, error) {
	ms := uint64(time.Now().UnixMilli())

	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("%w: %v", errReadRandom, err)
	}

	var out [ulidLen]byte

	// 48-bit 时间戳编码为 10 个 Base32 字符。第一个字符只使用低 3 位，
	// 因此合法 ULID 的首字符范围是 0..7。
	out[0] = ulidAlphabet[(ms>>45)&31]
	out[1] = ulidAlphabet[(ms>>40)&31]
	out[2] = ulidAlphabet[(ms>>35)&31]
	out[3] = ulidAlphabet[(ms>>30)&31]
	out[4] = ulidAlphabet[(ms>>25)&31]
	out[5] = ulidAlphabet[(ms>>20)&31]
	out[6] = ulidAlphabet[(ms>>15)&31]
	out[7] = ulidAlphabet[(ms>>10)&31]
	out[8] = ulidAlphabet[(ms>>5)&31]
	out[9] = ulidAlphabet[ms&31]

	// 80-bit 随机段编码为 16 个 Base32 字符。
	out[10] = ulidAlphabet[(entropy[0]>>3)&31]
	out[11] = ulidAlphabet[((entropy[0]<<2)|(entropy[1]>>6))&31]
	out[12] = ulidAlphabet[(entropy[1]>>1)&31]
	out[13] = ulidAlphabet[((entropy[1]<<4)|(entropy[2]>>4))&31]
	out[14] = ulidAlphabet[((entropy[2]<<1)|(entropy[3]>>7))&31]
	out[15] = ulidAlphabet[(entropy[3]>>2)&31]
	out[16] = ulidAlphabet[((entropy[3]<<3)|(entropy[4]>>5))&31]
	out[17] = ulidAlphabet[entropy[4]&31]
	out[18] = ulidAlphabet[(entropy[5]>>3)&31]
	out[19] = ulidAlphabet[((entropy[5]<<2)|(entropy[6]>>6))&31]
	out[20] = ulidAlphabet[(entropy[6]>>1)&31]
	out[21] = ulidAlphabet[((entropy[6]<<4)|(entropy[7]>>4))&31]
	out[22] = ulidAlphabet[((entropy[7]<<1)|(entropy[8]>>7))&31]
	out[23] = ulidAlphabet[(entropy[8]>>2)&31]
	out[24] = ulidAlphabet[((entropy[8]<<3)|(entropy[9]>>5))&31]
	out[25] = ulidAlphabet[entropy[9]&31]

	return string(out[:]), nil
}

// MustNewULID 在无法读取安全随机源时 panic，仅用于启动期不可恢复场景。
func MustNewULID() string {
	id, err := NewULID()
	if err != nil {
		panic(fmt.Sprintf("ids: %v", err))
	}
	return id
}
