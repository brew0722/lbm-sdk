package codec

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/tendermint/go-amino"
)

func EncodeFieldNumberAndTyp3(w io.Writer, num uint32, typ amino.Typ3) (err error) {
	if (typ & 0xF8) != 0 {
		panic(fmt.Sprintf("invalid Typ3 byte %v", typ))
	}
	if num > (1<<29 - 1) {
		panic(fmt.Sprintf("invalid field number %v", num))
	}

	// Pack Typ3 and field number.
	var value64 = (uint64(num) << 3) | uint64(typ)

	// Write uvarint value for field and Typ3.
	var buf [10]byte
	n := binary.PutUvarint(buf[:], value64)
	_, err = w.Write(buf[0:n])
	return
}

func decodeFieldNumberAndTyp3(bz []byte) (num uint32, typ amino.Typ3, n int, err error) {
	// Read uvarint value.
	var value64 uint64
	value64, n, err = amino.DecodeUvarint(bz)
	if err != nil {
		return
	}

	// Decode first typ3 byte.
	typ = amino.Typ3(value64 & 0x07)

	// Decode num.
	num64 := value64 >> 3
	if num64 > (1<<29 - 1) {
		err = fmt.Errorf("invalid field num %v", num64)
		return
	}
	num = uint32(num64)
	return
}

func CheckFieldNumberAndTyp3(bz []byte, expectedFnum uint32, expectedTyp amino.Typ3) (int, error) {
	fnum, typ, n, err := decodeFieldNumberAndTyp3(bz)
	if err != nil {
		return 0, err
	}
	if fnum != expectedFnum {
		return 0, nil
	}
	if typ != expectedTyp {
		return 0, fmt.Errorf("expected field type %v, got %v", expectedTyp, typ)
	}
	return n, nil
}

// CONTRACT: by the time this is called, len(bz) >= _n
// Returns true so you can write one-liners.
func Slide(bz *[]byte, n *int, _n int) bool {
	if _n < 0 || _n > len(*bz) {
		panic(fmt.Sprintf("impossible slide: len:%v _n:%v", len(*bz), _n))
	}
	*bz = (*bz)[_n:]
	if n != nil {
		*n += _n
	}
	return true
}

func EncodeFieldUvarint(w io.Writer, fnum uint32, u uint64) error {
	if u == 0 {
		return nil
	}
	if err := EncodeFieldNumberAndTyp3(w, fnum, amino.Typ3_Varint); err != nil {
		return err
	}
	if err := amino.EncodeUvarint(w, u); err != nil {
		return err
	}
	return nil
}

func EncodeFieldByteSlice(w io.Writer, fnum uint32, bz []byte) error {
	if len(bz) == 0 {
		return nil
	}
	if err := EncodeFieldNumberAndTyp3(w, fnum, amino.Typ3_ByteLength); err != nil {
		return err
	}
	if err := amino.EncodeByteSlice(w, bz); err != nil {
		return err
	}
	return nil
}

func DecodeFieldUvarint(bz []byte, fnum uint32) (u uint64, n int, err error) {
	_n, err := CheckFieldNumberAndTyp3(bz, fnum, amino.Typ3_Varint)
	if _n == 0 || err != nil {
		return u, _n, err
	}
	Slide(&bz, &n, _n)

	u, _n, err = amino.DecodeUvarint(bz)
	Slide(&bz, &n, _n)

	return u, n, err
}

func DecodeFieldByteSlice(bz []byte, fnum uint32) (bz2 []byte, n int, err error) {
	_n, err := CheckFieldNumberAndTyp3(bz, fnum, amino.Typ3_ByteLength)
	if _n == 0 || err != nil {
		return nil, _n, err
	}
	Slide(&bz, &n, _n)

	bz2, _n, err = amino.DecodeByteSlice(bz)
	Slide(&bz, &n, _n)

	return bz2, n, err
}