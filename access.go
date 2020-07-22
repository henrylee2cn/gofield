package gofield

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/henrylee2cn/ameda"
)

type (
	// Accessor struct accessor factory
	Accessor struct {
		dict     map[int32]*StructType // key is runtime type ID
		rw       sync.RWMutex
		groupBy  GroupByFunc
		iterator IteratorFunc
		maxDeep  int
	}
	// Struct struct accessor
	Struct struct {
		*StructType
		value       Value
		fieldValues []Value // idx is int
	}
	// StructType struct type info
	StructType struct {
		tid        int32
		fields     []*FieldType
		fieldGroup map[string][]*FieldType
		depth      int
		tree       *FieldType // id = -1
	}
	// FieldID id assigned to each field in sequence
	FieldID = int
	// FieldType field type info
	FieldType struct {
		id       int
		selector string
		deep     int
		ptrNum   int
		elemTyp  reflect.Type
		parent   *FieldType
		children []*FieldType
		reflect.StructField
	}
	// Value field value
	Value struct {
		elemVal reflect.Value
		elemPtr uintptr
	}
)

const rootID = -1

var (
	defaultAccessor = New()
	zero            = reflect.Value{}
	errTypeMismatch = errors.New("type mismatch")
	errIllegalType  = errors.New("type is not struct pointer")
)

// New create a new struct accessor factory.
func New(opt ...Option) *Accessor {
	a := &Accessor{
		dict:    make(map[int32]*StructType, 1024),
		maxDeep: 16,
	}
	for _, fn := range opt {
		fn(a)
	}
	return a
}

func (a *Accessor) load(tid int32) (*StructType, bool) {
	a.rw.RLock()
	sTyp, ok := a.dict[tid]
	a.rw.RUnlock()
	return sTyp, ok
}

func (a *Accessor) store(sTyp *StructType) {
	a.rw.Lock()
	a.dict[sTyp.tid] = sTyp
	a.rw.Unlock()
}

// MustAnalyze analyze the struct and return its type info.
// NOTE:
//  If structPtr is not a struct pointer, it will cause panic.
func MustAnalyze(structPtr interface{}) *StructType {
	return defaultAccessor.MustAnalyze(structPtr)
}

// MustAnalyze analyze the struct and return its type info.
// NOTE:
//  If structPtr is not a struct pointer, it will cause panic.
func (a *Accessor) MustAnalyze(structPtr interface{}) *StructType {
	s, err := a.Analyze(structPtr)
	if err != nil {
		panic(err)
	}
	return s
}

// Analyze analyze the struct and return its type info.
func Analyze(structPtr interface{}) (*StructType, error) {
	return defaultAccessor.Analyze(structPtr)
}

// Analyze analyze the struct and return its type info.
func (a *Accessor) Analyze(structPtr interface{}) (*StructType, error) {
	tid, _, err := parseStructInfoWithCheck(structPtr)
	if err != nil {
		return nil, err
	}
	return a.analyze(tid, structPtr), nil
}

func (a *Accessor) analyze(tid int32, structPtr interface{}) *StructType {
	sTyp, ok := a.load(tid)
	if !ok {
		sTyp = a.newStructType(tid, structPtr)
		a.store(sTyp)
	}
	return sTyp
}

// MustAccess analyze the struct type info and create struct accessor.
// NOTE:
//  If structPtr is not a struct pointer, it will cause panic.
func MustAccess(structPtr interface{}) *Struct {
	return defaultAccessor.MustAccess(structPtr)
}

// MustAccess analyze the struct type info and create struct accessor.
// NOTE:
//  If structPtr is not a struct pointer, it will cause panic.
func (a *Accessor) MustAccess(structPtr interface{}) *Struct {
	tid, ptr := parseStructInfo(structPtr)
	sTyp, ok := a.load(tid)
	if !ok {
		sTyp = a.newStructType(tid, structPtr)
		a.store(sTyp)
	}
	return newStruct(sTyp, ptr)
}

// Access analyze the struct type info and create struct accessor.
func Access(structPtr interface{}) (*Struct, error) {
	return defaultAccessor.Access(structPtr)
}

// Access analyze the struct type info and create struct accessor.
func (a *Accessor) Access(structPtr interface{}) (*Struct, error) {
	tid, ptr, err := parseStructInfoWithCheck(structPtr)
	if err != nil {
		return nil, err
	}
	sTyp, ok := a.load(tid)
	if !ok {
		sTyp = a.newStructType(tid, structPtr)
		a.store(sTyp)
	}
	return newStruct(sTyp, ptr), nil
}

// MustAccess create a new struct accessor.
// NOTE:
//  If structPtr is not a struct pointer or type mismatch, it will cause panic.
func (s *StructType) MustAccess(structPtr interface{}) *Struct {
	a, err := s.Access(structPtr)
	if err != nil {
		panic(err)
	}
	return a
}

// Access create a new struct accessor.
func (s *StructType) Access(structPtr interface{}) (*Struct, error) {
	tid, ptr := parseStructInfo(structPtr)
	if s.tid != tid {
		return nil, errTypeMismatch
	}
	return newStruct(s, ptr), nil
}

func newStruct(typ *StructType, elemPtr uintptr) *Struct {
	return &Struct{
		StructType: typ,
		value: Value{
			elemPtr: elemPtr,
		},
		fieldValues: make([]Value, len(typ.fields)),
	}
}

// Depth return the struct nesting depth(at least 1).
func (s *StructType) Depth() int {
	return s.depth
}

// RuntimeTypeID get the runtime type id of struct.
func (s *StructType) RuntimeTypeID() int32 {
	return s.tid
}

// NumField get the number of fields.
func (s *StructType) NumField() int {
	return len(s.fields)
}

// FieldType get the field type info corresponding to the id.
func (s *StructType) FieldType(id int) *FieldType {
	if !s.checkID(id) {
		return nil
	}
	return s.fields[id]
}

// Filter filter all fields and return a list of their ids.
func (s *StructType) Filter(fn func(*FieldType) bool) []int {
	list := make([]int, 0, s.NumField())
	for id, field := range s.fields {
		if fn(field) {
			list = append(list, id)
		}
	}
	return list
}

// FieldValue get the field value corresponding to the id.
func (s *Struct) FieldValue(id int) reflect.Value {
	if !s.checkID(id) {
		return zero
	}
	v := s.fieldValues[id]
	if v.elemPtr > 0 {
		return v.elemVal
	}
	return s.StructType.fields[id].init(s).elemVal
}

// Field get the field type and value corresponding to the id.
func (s *Struct) Field(id int) (*FieldType, reflect.Value) {
	if !s.checkID(id) {
		return nil, zero
	}
	t := s.StructType.fields[id]
	v := s.fieldValues[id]
	if v.elemPtr > 0 {
		return t, v.elemVal
	}
	return t, t.init(s).elemVal
}

// Range traverse all fields, and exit the traversal when fn returns false.
func (s *Struct) Range(fn func(*FieldType, reflect.Value) bool) {
	for id, t := range s.fields {
		v := s.fieldValues[id]
		if v.elemPtr > 0 {
			if !fn(t, v.elemVal) {
				return
			}
		} else {
			if !fn(t, t.init(s).elemVal) {
				return
			}
		}
	}
}

// GroupTypes return the field types by group.
func (s *StructType) GroupTypes(group string) []*FieldType {
	a := s.fieldGroup[group]
	return a
}

// GroupValues return the field values by group.
func (s *Struct) GroupValues(group string) []reflect.Value {
	a := s.StructType.GroupTypes(group)
	r := make([]reflect.Value, len(a))
	for i, ft := range a {
		v := s.fieldValues[ft.id]
		if v.elemPtr > 0 {
			r[i] = v.elemVal
		} else {
			r[i] = ft.init(s).elemVal
		}
	}
	return r
}

func (s *StructType) checkID(id int) bool {
	return id >= 0 && id < len(s.fields)
}

func (f *FieldType) init(s *Struct) Value {
	if f.parent == nil {
		return s.value // the original caller ensures that it has been initialized
	}
	v := s.fieldValues[f.id]
	if v.elemPtr > 0 {
		return v
	}
	pVal := f.parent.init(s)
	ptr := pVal.elemPtr + f.Offset
	valPtr := reflect.NewAt(f.StructField.Type, unsafe.Pointer(ptr))
	if f.ptrNum > 0 {
		valPtr = derefPtrAndInit(valPtr, f.ptrNum)
	}
	v = Value{
		elemVal: valPtr.Elem(),
		elemPtr: valPtr.Pointer(),
	}
	s.fieldValues[f.id] = v
	return v
}

func derefPtrAndInit(v reflect.Value, numPtr int) reflect.Value {
	for ; numPtr > 0; numPtr-- {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.IsNil() {
		v.Set(reflect.New(v.Type().Elem()))
	}
	return v
}

// ID get the field id.
func (f *FieldType) ID() int {
	return f.id
}

// Selector get the field full path.
func (f *FieldType) Selector() string {
	return f.selector
}

// String dump the id and selector on the field tree.
func (s *StructType) String() string {
	return s.Dump()
}

// Dump dump the id and selector on the field tree.
func (s *StructType) Dump() string {
	return s.tree.dump("")
}

// String dump the id and selector on the field subtree.
func (f *FieldType) String() string {
	return f.Dump()
}

// Dump dump the id and selector on the field subtree.
func (f *FieldType) Dump() string {
	return f.dump("")
}

func (f *FieldType) dump(prefix string) string {
	var buf bytes.Buffer
	if f.id != rootID {
		buf.WriteString(fmt.Sprintf("%sid=%d selector=%s\n", prefix, f.id, f.selector))
		prefix += "····"
	}
	for _, child := range f.children {
		buf.WriteString(child.dump(prefix))
	}
	return buf.String()
}

// Deep get the nesting depth of the field.
func (f *FieldType) Deep() int {
	return f.deep
}

// Kind get the field kind.
func (f *FieldType) Kind() reflect.Kind {
	return f.StructField.Type.Kind()
}

// UnderlyingKind get the underlying kind of the field
func (f *FieldType) UnderlyingKind() reflect.Kind {
	return f.elemTyp.Kind()
}

func (a *Accessor) newStructType(tid int32, structPtr interface{}) *StructType {
	v, ok := structPtr.(reflect.Value)
	if !ok {
		v = reflect.ValueOf(structPtr)
	}
	v = ameda.DereferencePtrValue(v)
	structTyp := v.Type()
	sTyp := &StructType{
		tid:    tid,
		fields: make([]*FieldType, 0, 16),
		tree:   &FieldType{id: rootID, elemTyp: structTyp},
	}
	sTyp.traversalFields(a.maxDeep, a.iterator, sTyp.tree)
	if a.groupBy != nil {
		sTyp.groupBy(a.groupBy)
	}
	return sTyp
}

func (s *StructType) traversalFields(maxFieldDeep int, iterator IteratorFunc, parent *FieldType) {
	if s.depth >= maxFieldDeep {
		return
	}
	s.depth++
	structTyp := parent.elemTyp
	numField := structTyp.NumField()
	var structFields []*FieldType
L:
	for i := 0; i < numField; i++ {
		f := structTyp.Field(i)
		elemTyp := f.Type
		var ptrNum int
		for elemTyp.Kind() == reflect.Ptr {
			elemTyp = elemTyp.Elem()
			ptrNum++
		}
		field := &FieldType{
			parent:      parent,
			id:          len(s.fields), // 0, 1, 2, ...
			selector:    joinFieldName(parent.selector, f.Name),
			deep:        s.depth,
			ptrNum:      ptrNum,
			elemTyp:     elemTyp,
			StructField: f,
		}
		if iterator != nil {
			switch p := iterator(field); p {
			default:
				fallthrough
			case Take, TakeAndStop:
				parent.children = append(parent.children, field)
				s.fields = append(s.fields, field)
				if elemTyp.Kind() == reflect.Struct {
					structFields = append(structFields, field)
				}
				if TakeAndStop == p {
					break L
				}
			case SkipOffspring, SkipOffspringAndStop:
				parent.children = append(parent.children, field)
				s.fields = append(s.fields, field)
				if SkipOffspringAndStop == p {
					break L
				}
			case Skip:
				continue L
			case SkipAndStop:
				break L
			}
		} else {
			parent.children = append(parent.children, field)
			s.fields = append(s.fields, field)
			if elemTyp.Kind() == reflect.Struct {
				structFields = append(structFields, field)
			}
		}
	}
	for _, field := range structFields {
		s.traversalFields(maxFieldDeep, iterator, field)
	}
}

func (s *StructType) groupBy(fn GroupByFunc) {
	s.fieldGroup = make(map[string][]*FieldType, len(s.fields))
	for _, field := range s.fields {
		group, ok := fn(field)
		if ok {
			a := s.fieldGroup[group]
			s.fieldGroup[group] = append(a, field)
		}
	}
}

// FieldTree return the field tree.
func (s *StructType) FieldTree() []*FieldType {
	return s.tree.children
}

// Parent return the parent field.
// NOTE:
//  may return nil
func (f *FieldType) Parent() *FieldType {
	if f.parent == nil || f.parent.id == rootID {
		return nil
	}
	return f.parent
}

// Children return the child fields.
func (f *FieldType) Children() []*FieldType {
	return f.children
}

func joinFieldName(parentPath, name string) string {
	return parentPath + "." + name
}
