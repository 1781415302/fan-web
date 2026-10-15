import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/services/app_updater.dart';

void main() {
  group('isSha256SumsAssetName', () {
    test('exact names match case-insensitively', () {
      expect(isSha256SumsAssetName('SHA256SUMS.txt'), isTrue);
      expect(isSha256SumsAssetName('sha256sums'), isTrue);
      expect(isSha256SumsAssetName('SHA256SUMS'), isTrue);
    });

    test('loose or prefixed names do not match', () {
      expect(isSha256SumsAssetName('sha256-evil.txt'), isFalse);
      expect(isSha256SumsAssetName('sha256sums.txt.sig'), isFalse);
      expect(isSha256SumsAssetName('my-sha256sums.txt'), isFalse);
      expect(isSha256SumsAssetName(''), isFalse);
    });
  });

  group('downloadAndInstallApk fail-closed without SHA256SUMS', () {
    test('missing sha256sumsUrl throws ApkIntegrityException', () async {
      await expectLater(
        downloadAndInstallApk('https://x/fan-web-app-v1.0.0.apk'),
        throwsA(
          isA<ApkIntegrityException>().having(
            (e) => e.toString(),
            'message',
            contains('SHA256SUMS.txt'),
          ),
        ),
      );
    });

    test('empty sha256sumsUrl throws ApkIntegrityException', () async {
      await expectLater(
        downloadAndInstallApk(
          'https://x/fan-web-app-v1.0.0.apk',
          sha256sumsUrl: '',
        ),
        throwsA(
          isA<ApkIntegrityException>().having(
            (e) => e.toString(),
            'message',
            contains('SHA256SUMS.txt'),
          ),
        ),
      );
    });
  });
}
