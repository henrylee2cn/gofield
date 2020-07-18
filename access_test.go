package gofield_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"

	"github.com/henrylee2cn/gofield"
)

type P1 struct {
	A int
	b int
	P2
}
type P2 struct {
	C int
	d int
	*P3
}
type P3 struct {
	E int
	f *int
	g **int
}

func BenchmarkAccess(b *testing.B) {
	var p P1
	s, err := gofield.Access(p)
	assert.EqualError(b, err, "type is not struct pointer")

	s, err = gofield.Access(&p)
	assert.NoError(b, err)
	b.ResetTimer()
	assert.Equal(b, 9, s.NumField())
	for i := 0; i < b.N; i++ {
		num := s.NumField()
		for i := 0; i < num; i++ {
			v := s.FieldValue(i)
			switch v.Kind() {
			case reflect.Int:
				v.SetInt(int64(i))
			case reflect.Struct:
			}
		}
	}
	b.StopTimer()
	assert.Equal(b, 0, p.A)
	assert.Equal(b, 1, p.b)
	assert.Equal(b, 3, p.C)
	assert.Equal(b, 4, p.d)
	assert.Equal(b, 6, p.E)
	assert.Equal(b, 7, *p.f)
	assert.Equal(b, 8, **p.g)
}

func BenchmarkReflect(b *testing.B) {
	var p P1
	s := reflect.ValueOf(&p)
	b.ResetTimer()
	s = s.Elem()
	assert.Equal(b, 3, s.NumField())
	var valInt int
	var setVal func(v reflect.Value)
	setVal = func(s reflect.Value) {
		num := s.NumField()
		for i := 0; i < num; i++ {
			f := s.Field(i)
			for f.Kind() == reflect.Ptr {
				if f.IsNil() {
					reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().
						Set(reflect.New(f.Type().Elem()))
				}
				f = f.Elem()
				if f.Kind() == reflect.Ptr && f.IsNil() {
					reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr()))
				}
			}
			switch f.Kind() {
			case reflect.Int:
				if f.CanSet() {
					f.SetInt(int64(valInt))
				} else {
					p := (*int)(unsafe.Pointer(f.UnsafeAddr()))
					*p = valInt
				}
				valInt++
			case reflect.Struct:
				valInt++
				setVal(f)
			}
		}
	}
	for i := 0; i < b.N; i++ {
		valInt = 0
		setVal(s)
	}
	b.StopTimer()
	assert.Equal(b, 0, p.A)
	assert.Equal(b, 1, p.b)
	assert.Equal(b, 3, p.C)
	assert.Equal(b, 4, p.d)
	assert.Equal(b, 6, p.E)
	assert.Equal(b, 7, *p.f)
	assert.Equal(b, 8, **p.g)
}
