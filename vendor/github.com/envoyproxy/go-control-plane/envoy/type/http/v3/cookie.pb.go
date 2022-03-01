// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.27.1
// 	protoc        v3.19.3
// source: envoy/type/http/v3/cookie.proto

package httpv3

import (
	_ "github.com/cncf/xds/go/udpa/annotations"
	_ "github.com/envoyproxy/protoc-gen-validate/validate"
	duration "github.com/golang/protobuf/ptypes/duration"
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

// Cookie defines an API for obtaining or generating HTTP cookie.
type Cookie struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The name that will be used to obtain cookie value from downstream HTTP request or generate
	// new cookie for downstream.
	Name string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	// Duration of cookie. This will be used to set the expiry time of a new cookie when it is
	// generated. Set this to 0 to use a session cookie.
	Ttl *duration.Duration `protobuf:"bytes,2,opt,name=ttl,proto3" json:"ttl,omitempty"`
	// Path of cookie. This will be used to set the path of a new cookie when it is generated.
	// If no path is specified here, no path will be set for the cookie.
	Path string `protobuf:"bytes,3,opt,name=path,proto3" json:"path,omitempty"`
}

func (x *Cookie) Reset() {
	*x = Cookie{}
	if protoimpl.UnsafeEnabled {
		mi := &file_envoy_type_http_v3_cookie_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Cookie) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Cookie) ProtoMessage() {}

func (x *Cookie) ProtoReflect() protoreflect.Message {
	mi := &file_envoy_type_http_v3_cookie_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Cookie.ProtoReflect.Descriptor instead.
func (*Cookie) Descriptor() ([]byte, []int) {
	return file_envoy_type_http_v3_cookie_proto_rawDescGZIP(), []int{0}
}

func (x *Cookie) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *Cookie) GetTtl() *duration.Duration {
	if x != nil {
		return x.Ttl
	}
	return nil
}

func (x *Cookie) GetPath() string {
	if x != nil {
		return x.Path
	}
	return ""
}

var File_envoy_type_http_v3_cookie_proto protoreflect.FileDescriptor

var file_envoy_type_http_v3_cookie_proto_rawDesc = []byte{
	0x0a, 0x1f, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2f, 0x74, 0x79, 0x70, 0x65, 0x2f, 0x68, 0x74, 0x74,
	0x70, 0x2f, 0x76, 0x33, 0x2f, 0x63, 0x6f, 0x6f, 0x6b, 0x69, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x12, 0x12, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x2e, 0x68, 0x74,
	0x74, 0x70, 0x2e, 0x76, 0x33, 0x1a, 0x1e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2f, 0x64, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x1d, 0x75, 0x64, 0x70, 0x61, 0x2f, 0x61, 0x6e, 0x6e, 0x6f,
	0x74, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x17, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x65, 0x2f, 0x76,
	0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x70, 0x0a,
	0x06, 0x43, 0x6f, 0x6f, 0x6b, 0x69, 0x65, 0x12, 0x1b, 0x0a, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x42, 0x07, 0xfa, 0x42, 0x04, 0x72, 0x02, 0x10, 0x01, 0x52, 0x04,
	0x6e, 0x61, 0x6d, 0x65, 0x12, 0x35, 0x0a, 0x03, 0x74, 0x74, 0x6c, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x0b, 0x32, 0x19, 0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x62, 0x75, 0x66, 0x2e, 0x44, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x42, 0x08, 0xfa, 0x42,
	0x05, 0xaa, 0x01, 0x02, 0x32, 0x00, 0x52, 0x03, 0x74, 0x74, 0x6c, 0x12, 0x12, 0x0a, 0x04, 0x70,
	0x61, 0x74, 0x68, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x70, 0x61, 0x74, 0x68, 0x42,
	0x7b, 0x0a, 0x20, 0x69, 0x6f, 0x2e, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x70, 0x72, 0x6f, 0x78, 0x79,
	0x2e, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x2e, 0x68, 0x74, 0x74, 0x70,
	0x2e, 0x76, 0x33, 0x42, 0x0b, 0x43, 0x6f, 0x6f, 0x6b, 0x69, 0x65, 0x50, 0x72, 0x6f, 0x74, 0x6f,
	0x50, 0x01, 0x5a, 0x40, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x65,
	0x6e, 0x76, 0x6f, 0x79, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x2f, 0x67, 0x6f, 0x2d, 0x63, 0x6f, 0x6e,
	0x74, 0x72, 0x6f, 0x6c, 0x2d, 0x70, 0x6c, 0x61, 0x6e, 0x65, 0x2f, 0x65, 0x6e, 0x76, 0x6f, 0x79,
	0x2f, 0x74, 0x79, 0x70, 0x65, 0x2f, 0x68, 0x74, 0x74, 0x70, 0x2f, 0x76, 0x33, 0x3b, 0x68, 0x74,
	0x74, 0x70, 0x76, 0x33, 0xba, 0x80, 0xc8, 0xd1, 0x06, 0x02, 0x10, 0x02, 0x62, 0x06, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_envoy_type_http_v3_cookie_proto_rawDescOnce sync.Once
	file_envoy_type_http_v3_cookie_proto_rawDescData = file_envoy_type_http_v3_cookie_proto_rawDesc
)

func file_envoy_type_http_v3_cookie_proto_rawDescGZIP() []byte {
	file_envoy_type_http_v3_cookie_proto_rawDescOnce.Do(func() {
		file_envoy_type_http_v3_cookie_proto_rawDescData = protoimpl.X.CompressGZIP(file_envoy_type_http_v3_cookie_proto_rawDescData)
	})
	return file_envoy_type_http_v3_cookie_proto_rawDescData
}

var file_envoy_type_http_v3_cookie_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_envoy_type_http_v3_cookie_proto_goTypes = []interface{}{
	(*Cookie)(nil),            // 0: envoy.type.http.v3.Cookie
	(*duration.Duration)(nil), // 1: google.protobuf.Duration
}
var file_envoy_type_http_v3_cookie_proto_depIdxs = []int32{
	1, // 0: envoy.type.http.v3.Cookie.ttl:type_name -> google.protobuf.Duration
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_envoy_type_http_v3_cookie_proto_init() }
func file_envoy_type_http_v3_cookie_proto_init() {
	if File_envoy_type_http_v3_cookie_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_envoy_type_http_v3_cookie_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Cookie); i {
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
			RawDescriptor: file_envoy_type_http_v3_cookie_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_envoy_type_http_v3_cookie_proto_goTypes,
		DependencyIndexes: file_envoy_type_http_v3_cookie_proto_depIdxs,
		MessageInfos:      file_envoy_type_http_v3_cookie_proto_msgTypes,
	}.Build()
	File_envoy_type_http_v3_cookie_proto = out.File
	file_envoy_type_http_v3_cookie_proto_rawDesc = nil
	file_envoy_type_http_v3_cookie_proto_goTypes = nil
	file_envoy_type_http_v3_cookie_proto_depIdxs = nil
}
