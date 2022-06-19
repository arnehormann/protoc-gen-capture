package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const usage = `Support capture, replaying and manipulation of protoc request.

Call it as a protoc-plugin to capture code generation requests:
  protoc --capture_out=. ...
  will create a file out.proto.msg in the current directory.
  For sensible values of ..., that is.

To support usage as a plugin, --wrap is true by default.
Unset it if you do not want to convert input requests to responses.
Like when you intend to pipe it to test your plugin:

Use it to test a plugin independent of protoc (result as json):
  < cgreq.proto.msg \
  PLUGIN \
  | protoc_gen_capture -wrap=false -json-out \
  > generation-response.json

You can also convert the code generation request to json:
  < cgreq.proto.msg \
  protoc_gen_capture -req-in=false -wrap=false -json-out \
  > generation-request.json

This enables you to diff results of various program versions.

NOTE:
This program might not be lossless.
It will always decode and reencode.
Unknown message parts will not be visible and might get dropped.

Decoding for responses is shallow. Included files - if proto -
will not be decoded.
`

func main() {
	err := run()
	if err != nil {
		log.Printf("%v\n", err)
	}
}

func run() error {
	var (
		help    = false
		file    = "out.proto.msg"
		jsonIn  = false
		jsonOut = false
		reqIn   = true
		wrap    = true
	)

	flag.CommandLine.Init(flag.CommandLine.Name(), flag.ContinueOnError)

	flag.BoolVar(&help, "help", help, "show this help text")
	flag.StringVar(&file, "file", file, "only if wrap is true: file name inside code generator response")

	flag.BoolVar(&jsonIn, "json-in", jsonIn, "input is json, else binary proto")
	flag.BoolVar(&jsonOut, "json-out", jsonOut, "output as json, else deterministic binary proto")

	flag.BoolVar(&reqIn, "req-in", reqIn, "input is request, not response")
	flag.BoolVar(&wrap, "wrap", wrap, "wrap input in response with filename "+file)

	flag.Parse()

	if help {
		flag.CommandLine.SetOutput(os.Stdout)
		fmt.Fprint(os.Stdout, usage)
		fmt.Fprint(os.Stdout, "\nArguments:\n")
		flag.PrintDefaults()
		return nil
	}

	bin, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("CodeGenerationRequest could not be read from stdin: %v", err)
	}

	var msg proto.Message
	if reqIn {
		msg = &pluginpb.CodeGeneratorRequest{}
	} else {
		msg = &pluginpb.CodeGeneratorResponse{}
	}

	var format string
	if jsonIn {
		format = "json"
		err = protojson.Unmarshal(bin, msg)
	} else {
		format = "proto"
		if reqIn {
			// custom unmarshal for requests to also cover extensions
			msg, err = unmarshalRequest(bin)
		} else {
			err = proto.Unmarshal(bin, msg)
		}
	}
	if err != nil {
		return fmt.Errorf("%s unmarshal error: %v", format, err)
	}

	encode := func(msg proto.Message, asJSON bool) ([]byte, error) {
		var format string
		var out []byte
		if asJSON {
			format = "json"
			out, err = protojson.MarshalOptions{
				Multiline:     true,
				Indent:        "\t",
				UseProtoNames: true,
			}.Marshal(msg)
		} else {
			format = "proto"
			out, err = proto.MarshalOptions{
				Deterministic: true,
			}.Marshal(msg)
		}
		if err != nil {
			err = fmt.Errorf("%s marshal error: %v", format, err)
		}
		return out, err
	}
	out, err := encode(msg, jsonOut)
	if err != nil {
		return err
	}
	if wrap {
		feat := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		resp := &pluginpb.CodeGeneratorResponse{
			File: []*pluginpb.CodeGeneratorResponse_File{
				{
					Name:    proto.String(file),
					Content: proto.String(string(out)),
				},
			},
			SupportedFeatures: &feat,
		}
		out, err = encode(resp, jsonOut)
		if err != nil {
			return fmt.Errorf("code generation response error: %v", err)
		}
	}

	_, err = os.Stdout.Write(out)
	if err != nil {
		// this is probably nonsensical :-)
		return fmt.Errorf("output error: %v", err)
	}
	return nil
}

// the following code supports proto unmarshaling with extensions

type typeRegistry struct {
	*protoregistry.Types
}

func (tr *typeRegistry) addEnums(d protoreflect.EnumDescriptors) error {
	for i, max := 0, d.Len(); i < max; i++ {
		err := tr.RegisterEnum(dynamicpb.NewEnumType(d.Get(i)))
		if err != nil {
			return err
		}
	}
	return nil
}

func (tr *typeRegistry) addExtensions(d protoreflect.ExtensionDescriptors) error {
	for i, max := 0, d.Len(); i < max; i++ {
		ext := d.Get(i)
		extTypeDesc, ok := ext.(protoreflect.ExtensionTypeDescriptor)
		if ok {
			err := tr.RegisterExtension(extTypeDesc.Type())
			if err != nil {
				return err
			}
			continue
		}
		err := tr.RegisterExtension(dynamicpb.NewExtensionType(d.Get(i)))
		if err != nil {
			return err
		}
	}
	return nil
}

func (tr *typeRegistry) addMessages(d protoreflect.MessageDescriptors) error {
	for i, max := 0, d.Len(); i < max; i++ {
		m := d.Get(i)
		err := tr.RegisterMessage(dynamicpb.NewMessageType(m))
		if err != nil {
			return err
		}
		// add inner types
		if err := tr.addEnums(m.Enums()); err != nil {
			return err
		}
		if err := tr.addExtensions(m.Extensions()); err != nil {
			return err
		}
		if err := tr.addMessages(m.Messages()); err != nil {
			return err
		}
	}
	return nil
}

func protoTypes(fileDescs []*descriptorpb.FileDescriptorProto) (*protoregistry.Types, error) {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: fileDescs})
	if err != nil {
		return nil, err
	}
	tr := typeRegistry{Types: &protoregistry.Types{}}
	files.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		if err = tr.addEnums(f.Enums()); err != nil {
			return false
		}
		if err = tr.addExtensions(f.Extensions()); err != nil {
			return false
		}
		if err = tr.addMessages(f.Messages()); err != nil {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return tr.Types, nil
}

func unmarshalRequest(raw []byte) (*pluginpb.CodeGeneratorRequest, error) {
	req := &pluginpb.CodeGeneratorRequest{}
	err := proto.Unmarshal(raw, req)
	if err != nil {
		return nil, fmt.Errorf("CodeGenerationRequest unmarshal failed: %v", err)
	}
	types, err := protoTypes(req.ProtoFile)
	if err != nil {
		return nil, fmt.Errorf("CodeGenerationRequest types could not be loaded: %v", err)
	}
	// unmarshal a second time to also resolve extensions
	req = &pluginpb.CodeGeneratorRequest{}
	err = proto.UnmarshalOptions{
		Resolver: types,
	}.Unmarshal(raw, req)
	if err != nil {
		return nil, fmt.Errorf("CodeGenerationRequest types could not be resolved: %v", err)
	}
	return req, nil
}
