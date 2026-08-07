import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/providers/player_provider.dart';
import 'package:fan_web/screens/player_screen.dart';

void main() {
  test('buildStreamUrl normalizes the server URL and encodes the token', () {
    expect(
      buildStreamUrl(' http://127.0.0.1:8080/// ', 42, 'jwt+/token'),
      'http://127.0.0.1:8080/api/episodes/42/stream?token=jwt%2B%2Ftoken',
    );
  });

  test('buildStreamMedia passes a positive resume position to media_kit', () {
    final media = buildStreamMedia('http://127.0.0.1:8080', 42, 'token', 125);

    expect(media.start, const Duration(seconds: 125));
  });

  test('buildStreamMedia leaves start unset without saved progress', () {
    final media = buildStreamMedia('http://127.0.0.1:8080', 42, 'token', 0);

    expect(media.start, isNull);
  });

  test('formatTime supports short and long durations', () {
    expect(formatTime(0), '00:00');
    expect(formatTime(125), '02:05');
    expect(formatTime(3661), '01:01:01');
    expect(formatTime(-1), '00:00');
  });
}
