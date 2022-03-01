// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.27.1
// 	protoc        v3.19.3
// source: envoy/extensions/resource_monitors/fixed_heap/v3/fixed_heap.proto

package fixed_heapv3

import (
	_ "github.com/cncf/xds/go/udpa/annotations"
	_ "github.com/envoyproxy/protoc-gen-validate/validate"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// The fixed heap resource monitor reports the Envoy process memory pressure, computed as a
// fraction of currently reserved heap memory divided by a statically configured maximum
// specified in the FixedHeapConfig.
type FixedHeapConfig struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	MaxHeapSizeBytes uint64 `protobuf:"varint,1,opt,name=max_heap_size_bytes,json=maxHeapSizeBytes,proto3" json:"max_heap_size_bytes,omitempty"`
}

func (x *FixedHeapConfig) Reset() {
	*x = FixedHeapConfig{}
	if protoimpl.UnsafeEnabled {
		mi := &file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *FixedHeapConfig) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FixedHeapConfig) ProtoMessage() {}

func (x *FixedHeapConfig) ProtoReflect() protoreflect.Message {
	mi := &file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FixedHeapConfig.ProtoReflect.Descriptor instead.
func (*FixedHeapConfig) Descriptor() ([]byte, []int) {
	return file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescGZIP(), []int{0}
}

func (x *FixedHeapConfig) GetMaxHeapSizeBytes() uint64 {
	if x != nil {
		return x.MaxHeapSizeBytes
	}
	return 0
}

var File_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto protoreflect.FileDescriptor

var file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDesc = []byte{
	0x0a, 0x41, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2f, 0x65, 0x78, 0x74, 0x65, 0x6e, 0x73, 0x69, 0x6f,
	0x6e, 0x73, 0x2f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x6e, 0x69,
	0x74, 0x6f, 0x72, 0x73, 0x2f, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65, 0x61, 0x70, 0x2f,
	0x76, 0x33, 0x2f, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65, 0x61, 0x70, 0x2e, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x12, 0x30, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x65, 0x78, 0x74, 0x65, 0x6e,
	0x73, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6d,
	0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x73, 0x2e, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65,
	0x61, 0x70, 0x2e, 0x76, 0x33, 0x1a, 0x1d, 0x75, 0x64, 0x70, 0x61, 0x2f, 0x61, 0x6e, 0x6e, 0x6f,
	0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x21, 0x75, 0x64, 0x70, 0x61, 0x2f, 0x61, 0x6e, 0x6e, 0x6f, 0x74,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x69, 0x6e,
	0x67, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x17, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74,
	0x65, 0x2f, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x22, 0x92, 0x01, 0x0a, 0x0f, 0x46, 0x69, 0x78, 0x65, 0x64, 0x48, 0x65, 0x61, 0x70, 0x43, 0x6f,
	0x6e, 0x66, 0x69, 0x67, 0x12, 0x36, 0x0a, 0x13, 0x6d, 0x61, 0x78, 0x5f, 0x68, 0x65, 0x61, 0x70,
	0x5f, 0x73, 0x69, 0x7a, 0x65, 0x5f, 0x62, 0x79, 0x74, 0x65, 0x73, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x04, 0x42, 0x07, 0xfa, 0x42, 0x04, 0x32, 0x02, 0x20, 0x00, 0x52, 0x10, 0x6d, 0x61, 0x78, 0x48,
	0x65, 0x61, 0x70, 0x53, 0x69, 0x7a, 0x65, 0x42, 0x79, 0x74, 0x65, 0x73, 0x3a, 0x47, 0x9a, 0xc5,
	0x88, 0x1e, 0x42, 0x0a, 0x40, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x63, 0x6f, 0x6e, 0x66, 0x69,
	0x67, 0x2e, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74,
	0x6f, 0x72, 0x2e, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65, 0x61, 0x70, 0x2e, 0x76, 0x32,
	0x61, 0x6c, 0x70, 0x68, 0x61, 0x2e, 0x46, 0x69, 0x78, 0x65, 0x64, 0x48, 0x65, 0x61, 0x70, 0x43,
	0x6f, 0x6e, 0x66, 0x69, 0x67, 0x42, 0xc0, 0x01, 0x0a, 0x3e, 0x69, 0x6f, 0x2e, 0x65, 0x6e, 0x76,
	0x6f, 0x79, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x2e, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x65, 0x78,
	0x74, 0x65, 0x6e, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63,
	0x65, 0x5f, 0x6d, 0x6f, 0x6e, 0x69, 0x74, 0x6f, 0x72, 0x73, 0x2e, 0x66, 0x69, 0x78, 0x65, 0x64,
	0x5f, 0x68, 0x65, 0x61, 0x70, 0x2e, 0x76, 0x33, 0x42, 0x0e, 0x46, 0x69, 0x78, 0x65, 0x64, 0x48,
	0x65, 0x61, 0x70, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x50, 0x01, 0x5a, 0x64, 0x67, 0x69, 0x74, 0x68,
	0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x70, 0x72, 0x6f, 0x78,
	0x79, 0x2f, 0x67, 0x6f, 0x2d, 0x63, 0x6f, 0x6e, 0x74, 0x72, 0x6f, 0x6c, 0x2d, 0x70, 0x6c, 0x61,
	0x6e, 0x65, 0x2f, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2f, 0x65, 0x78, 0x74, 0x65, 0x6e, 0x73, 0x69,
	0x6f, 0x6e, 0x73, 0x2f, 0x72, 0x65, 0x73, 0x6f, 0x75, 0x72, 0x63, 0x65, 0x5f, 0x6d, 0x6f, 0x6e,
	0x69, 0x74, 0x6f, 0x72, 0x73, 0x2f, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65, 0x61, 0x70,
	0x2f, 0x76, 0x33, 0x3b, 0x66, 0x69, 0x78, 0x65, 0x64, 0x5f, 0x68, 0x65, 0x61, 0x70, 0x76, 0x33,
	0xba, 0x80, 0xc8, 0xd1, 0x06, 0x02, 0x10, 0x02, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescOnce sync.Once
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescData = file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDesc
)

func file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescGZIP() []byte {
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescOnce.Do(func() {
		file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescData = protoimpl.X.CompressGZIP(file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescData)
	})
	return file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDescData
}

var file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_goTypes = []interface{}{
	(*FixedHeapConfig)(nil), // 0: envoy.extensions.resource_monitors.fixed_heap.v3.FixedHeapConfig
}
var file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_depIdxs = []int32{
	0, // [0:0] is the sub-list for method output_type
	0, // [0:0] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_init() }
func file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_init() {
	if File_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*FixedHeapConfig); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_goTypes,
		DependencyIndexes: file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_depIdxs,
		MessageInfos:      file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_msgTypes,
	}.Build()
	File_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto = out.File
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_rawDesc = nil
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_goTypes = nil
	file_envoy_extensions_resource_monitors_fixed_heap_v3_fixed_heap_proto_depIdxs = nil
}
