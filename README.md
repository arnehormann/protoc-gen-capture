# protoc-gen-capture

Support capture, replaying and manipulation of protoc request to simplify plugin development and make them more testable.

It can act as a protoc plugin. That's why name has to start with `protoc-gen-` - to make it discoverable by protoc. It will by default wrap an incoming CodeGenerationRequest in a CodeGenerationResponse and store it as `out.proto.msg`.

It can also convert CodeGenerationRequest and CodeGenerationResponse into json (and convert from json to proto).

With the stored request, you can do additional things:
* pipe it into you plugin:
  `<out.proto.msg PLUGIN | protoc-gen-capture -wrap=false > response.proto.msg`
* inspect the request:
  `<out.proto.msg protoc-gen-capture -wrap=false -json-out > request.proto.json`
* inspect the response:
  `<out.proto.msg protoc-gen-capture -wrap=false -req-in=false -json-out > response.proto.json`
* ... and of course, store various versions of the above and use them for plugin regression testing.

Here's the output of `protoc-gen-capture --help`:

```
Support capture, replaying and manipulation of protoc request.

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

Arguments:
  -file string
        only if wrap is true: file name inside code generator response (default "out.proto.msg")
  -help
        show this help text
  -json-in
        input is json, else binary proto
  -json-out
        output as json, else deterministic binary proto
  -req-in
        input is request, not response (default true)
  -wrap
        wrap input in response with filename out.proto.msg (default true)
```