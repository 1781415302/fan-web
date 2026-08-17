import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/services/app_updater.dart';

void main() {
  group('requireApkSha256', () {
    test('null or empty URL is size-only', () {
      expect(requireApkSha256(null), isFalse);
      expect(requireApkSha256(''), isFalse);
    });

    test('non-empty URL requires APK line and fail-closed fetch', () {
      expect(requireApkSha256('https://example.com/SHA256SUMS.txt'), isTrue);
    });
  });
}
