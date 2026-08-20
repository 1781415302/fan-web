import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';

import 'package:fan_web/api/api_client.dart';

class RecordingAdapter implements HttpClientAdapter {
  RecordingAdapter(this._handler);

  final ResponseBody Function(RequestOptions options) _handler;
  final List<RequestOptions> requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    return _handler(options);
  }

  @override
  void close({bool force = false}) {}
}

ApiClient clientWith(
  Map<String, dynamic> envelope, {
  int statusCode = 200,
  List<RequestOptions>? sink,
}) {
  return clientWithHandler((options) {
    sink?.add(options);
    return jsonBody(envelope, statusCode: statusCode);
  });
}

ApiClient clientWithHandler(
  ResponseBody Function(RequestOptions options) handler,
) {
  final dio = Dio();
  dio.httpClientAdapter = RecordingAdapter(handler);
  return ApiClient(dio: dio);
}

ResponseBody jsonBody(Object envelope, {int statusCode = 200}) {
  return ResponseBody.fromString(
    jsonEncode(envelope),
    statusCode,
    headers: <String, List<String>>{
      Headers.contentTypeHeader: <String>['application/json'],
    },
  );
}

Map<String, dynamic> ok(Object? data) => <String, dynamic>{
  'code': 0,
  'message': 'ok',
  'data': data,
};

Map<String, dynamic> errorEnvelope(int code, String message) =>
    <String, dynamic>{'code': code, 'message': message, 'data': null};
