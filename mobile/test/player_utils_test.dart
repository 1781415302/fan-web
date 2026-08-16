import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/screens/player_screen.dart';

void main() {
  test('formatTime supports short and long durations', () {
    expect(formatTime(0), '00:00');
    expect(formatTime(125), '02:05');
    expect(formatTime(3661), '01:01:01');
    expect(formatTime(-1), '00:00');
  });
}
