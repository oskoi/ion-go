package ion

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"math/big"
	"strings"
	"time"
)

// A Reader reads a stream of Ion values.
//
// The Reader has a logical position within the stream of values, influencing the
// values returnedd from its methods. Initially, the Reader is positioned before the
// first value in the stream. A call to Next advances the Reader to the first value
// in the stream, with subsequent calls advancing to subsequent values. When a call to
// Next moves the Reader to the position after the final value in the stream, it returns
// false, making it easy to loop through the values in a stream.
//
// 	var r Reader
// 	for r.Next() {
// 		// ...
// 	}
//
// Next also returns false in case of error. This can be distinguished from a legitimate
// end-of-stream by calling Err after exiting the loop.
//
// When positioned on an Ion value, the type of the value can be retrieved by calling
// Type. If it has an associated field name (inside a struct) or annotations, they can
// be read by calling FieldName and Annotations respectively.
//
// For atomic values, an appropriate XxxValue method can be called to read the value.
// For lists, sexps, and structs, you should instead call StepIn to move the Reader in
// to the contained sequence of values. The Reader will initially be positioned before
// the first value in the container. Calling Next without calling StepIn will skip over
// the composite value and return the next value in the outer value stream.
//
// At any point while reading through a composite value, including when Next returns false
// to indicate the end of the contained values, you may call StepOut to move back to the
// outer sequence of values. The Reader will be positioned at the end of the composite value,
// such that a call to Next will move to the immediately-following value (if any).
//
// 	r := NewTextReaderStr("[foo, bar] [baz]")
// 	for r.Next() {
// 		if err := r.StepIn(); err != nil {
// 			return err
// 		}
// 		for r.Next() {
// 			fmt.Println(r.StringValue())
// 		}
// 		if err := r.StepOut(); err != nil {
// 			return err
// 		}
// 	}
// 	if err := r.Err(); err != nil {
// 		return err
// 	}
//
type Reader interface {

	// SymbolTable returns the current symbol table, or nil if there isn't one.
	// Text Readers do not, generally speaking, have an associated symbol table.
	// Binary Readers do.
	SymbolTable() SymbolTable

	// Next advances the Reader to the next position in the current value stream.
	// It returns true if this is the position of an Ion value, and false if it
	// is not. On error, it returns false and sets Err.
	Next() bool

	// Err returns an error if a previous call call to Next has failed.
	Err() error

	// Type returns the type of the Ion value the Reader is currently positioned on.
	// It returns NoType if the Reader is positioned before or after a value.
	Type() Type

	// IsNull returns true if the current value is an explicit null. This may be true
	// even if the Type is not NullType (for example, null.struct has type Struct). Yes,
	// that's a bit confusing.
	IsNull() bool

	// FieldName returns the field name associated with the current value. It returns
	// the empty string if there is no current value or the current value has no field
	// name.
	FieldName() string

	// Annotations returns the set of annotations associated with the current value.
	// It returns nil if there is no current value or the current value has no annotations.
	Annotations() []string

	// StepIn steps in to the current value if it is a container. It returns an error if there
	// is no current value or if the value is not a container. On success, the Reader is
	// positioned before the first value in the container.
	StepIn() error

	// StepOut steps out of the current container value being read. It returns an error if
	// this Reader is not currently stepped in to a container. On success, the Reader is
	// positioned after the end of the container, but before any subsequent values in the
	// stream.
	StepOut() error

	// BoolValue returns the current value as a boolean (if that makes sense). It returns
	// an error if the current value is not an Ion bool.
	BoolValue() (bool, error)

	// IntSize returns the size of integer needed to losslessly represent the current value
	// (if that makes sense). It returns an error if the current value is not an Ion int.
	IntSize() (IntSize, error)

	// IntValue returns the current value as a 32-bit integer (if that makes sense). It
	// returns an error if the current value is not an Ion integer or requires more than
	// 32 bits to represent losslessly.
	IntValue() (int, error)

	// Int64Value returns the current value as a 64-bit integer (if that makes sense). It
	// returns an error if the current value is not an Ion integer or requires more than
	// 64 bits to represent losslessly.
	Int64Value() (int64, error)

	// BigIntValue returns the current value as a big.Integer (if that makes sense). It
	// returns an error if the current value is not an Ion integer.
	BigIntValue() (*big.Int, error)

	// FloatValue returns the current value as a 64-bit floating point number (if that
	// makes sense). It returns an error if the current value is not an Ion float.
	FloatValue() (float64, error)

	// DecimalValue returns the current value as an arbitrary-precision Decimal (if that
	// makes sense). It returns an error if the current value is not an Ion decimal.
	DecimalValue() (*Decimal, error)

	// TimeValue returns the current value as a timestamp (if that makes sense). It returns
	// an error if the current value is not an Ion timestamp.
	TimeValue() (time.Time, error)

	// StringValue returns the current value as a string (if that makes sense). It returns
	// an error if the current value is not an Ion symbol or an Ion string.
	StringValue() (string, error)

	// ByteValue returns the current value as a byte slice (if that makes sense). It returns
	// an error if the current value is not an Ion clob or an Ion blob.
	ByteValue() ([]byte, error)
}

// NewReader creates a new Ion reader of the appropriate type by peeking
// at the first several bytes of input for a binary version marker.
func NewReader(in io.Reader) Reader {
	return newReader(in, nil)
}

// NewReaderStr creates a new reader from a string.
func NewReaderStr(str string) Reader {
	return NewReader(strings.NewReader(str))
}

// NewReaderBytes creates a new reader for the given bytes.
func NewReaderBytes(in []byte) Reader {
	return NewReader(bytes.NewReader(in))
}

func newReader(in io.Reader, cat *Catalog) Reader {
	br := bufio.NewReader(in)

	bs, err := br.Peek(4)
	if err == nil && bs[0] == 0xE0 && bs[3] == 0xEA {
		return newBinaryReaderBuf(br, cat)
	}

	return newTextReaderBuf(br)
}

// A reader holds common implementation stuff to both the text and binary readers.
type reader struct {
	ctx ctxstack
	eof bool
	err error

	fieldName   string
	annotations []string
	valueType   Type
	value       interface{}
}

// Err returns the current error.
func (r *reader) Err() error {
	return r.err
}

// Type returns the current value's type.
func (r *reader) Type() Type {
	return r.valueType
}

// IsNull returns true if the current value is null.
func (r *reader) IsNull() bool {
	return r.valueType != NoType && r.value == nil
}

// FieldName returns the current value's field name.
func (r *reader) FieldName() string {
	return r.fieldName
}

// Annotations returns the current value's annotations.
func (r *reader) Annotations() []string {
	return r.annotations
}

// BoolValue returns the current value as a bool.
func (r *reader) BoolValue() (bool, error) {
	if r.valueType == BoolType {
		if r.value == nil {
			return false, nil
		}
		return r.value.(bool), nil
	}
	return false, errors.New("ion: value is not a bool")
}

// IntSize returns the size of the current int value.
func (r *reader) IntSize() (IntSize, error) {
	if r.valueType != IntType {
		return NullInt, errors.New("ion: value is not an int")
	}
	if r.value == nil {
		return NullInt, nil
	}

	if i, ok := r.value.(int64); ok {
		if i > math.MaxInt32 || i < math.MinInt32 {
			return Int64, nil
		}
		return Int32, nil
	}

	return BigInt, nil
}

// IntValue returns the current value as an int.
func (r *reader) IntValue() (int, error) {
	i, err := r.Int64Value()
	if err != nil {
		return 0, err
	}
	if i > math.MaxInt32 || i < math.MinInt32 {
		return 0, errors.New("ion: int value out of bounds")
	}
	return int(i), nil
}

// Int64Value returns the current value as an int64.
func (r *reader) Int64Value() (int64, error) {
	if r.valueType == IntType {
		if r.value == nil {
			return 0, nil
		}

		if i, ok := r.value.(int64); ok {
			return i, nil
		}

		bi := r.value.(*big.Int)
		if bi.IsInt64() {
			return bi.Int64(), nil
		}

		return 0, errors.New("ion: int value out of bounds")
	}
	return 0, errors.New("ion: value is not an int")
}

// BigIntValue returns the current value as a big int.
func (r *reader) BigIntValue() (*big.Int, error) {
	if r.valueType == IntType {
		if r.value == nil {
			return nil, nil
		}
		if i, ok := r.value.(int64); ok {
			return big.NewInt(i), nil
		}
		return r.value.(*big.Int), nil
	}
	return nil, errors.New("ion: value is not an int")
}

// FloatValue returns the current value as a float.
func (r *reader) FloatValue() (float64, error) {
	if r.valueType == FloatType {
		if r.value == nil {
			return 0.0, nil
		}
		return r.value.(float64), nil
	}
	return 0.0, errors.New("ion: value is not a float")
}

// DecimalValue returns the current value as a Decimal.
func (r *reader) DecimalValue() (*Decimal, error) {
	if r.valueType == DecimalType {
		if r.value == nil {
			return nil, nil
		}
		return r.value.(*Decimal), nil
	}
	return nil, errors.New("ion: value is not a decimal")
}

// TimeValue returns the current value as a time.
func (r *reader) TimeValue() (time.Time, error) {
	if r.valueType == TimestampType {
		if r.value == nil {
			return time.Time{}, nil
		}
		return r.value.(time.Time), nil
	}
	return time.Time{}, errors.New("ion: value is not a timestamp")
}

// StringValue returns the current value as a string.
func (r *reader) StringValue() (string, error) {
	if r.valueType == StringType || r.valueType == SymbolType {
		if r.value == nil {
			return "", nil
		}
		return r.value.(string), nil
	}
	return "", errors.New("ion: value is not a string")
}

// ByteValue returns the current value as a byte slice.
func (r *reader) ByteValue() ([]byte, error) {
	if r.valueType == BlobType || r.valueType == ClobType {
		if r.value == nil {
			return nil, nil
		}
		return r.value.([]byte), nil
	}
	return nil, errors.New("ion: value is not a byte array")
}

func (r *reader) clear() {
	r.fieldName = ""
	r.annotations = nil
	r.valueType = NoType
	r.value = nil
}
