import 'package:flutter_test/flutter_test.dart';

import 'package:fan_web/api/api_client.dart';

void main() {
  group('ApiClient.normalizeServerUrl', () {
    test('prepends http:// when protocol is missing', () {
      expect(
        ApiClient.normalizeServerUrl('192.168.1.100:8080'),
        'http://192.168.1.100:8080',
      );
    });

    test('preserves https and strips trailing slashes', () {
      expect(
        ApiClient.normalizeServerUrl('https://fan.example.com/'),
        'https://fan.example.com',
      );
    });

    test('strips multiple trailing slashes', () {
      expect(
        ApiClient.normalizeServerUrl('http://localhost:8080///'),
        'http://localhost:8080',
      );
    });

    test('trims whitespace', () {
      expect(
        ApiClient.normalizeServerUrl('  http://localhost:8080  '),
        'http://localhost:8080',
      );
    });

    test('throws FormatException for empty string', () {
      expect(
        () => ApiClient.normalizeServerUrl(''),
        throwsA(isA<FormatException>()),
      );
    });

    test('throws FormatException for whitespace only', () {
      expect(
        () => ApiClient.normalizeServerUrl('   '),
        throwsA(isA<FormatException>()),
      );
    });

    test('throws FormatException for no host', () {
      expect(
        () => ApiClient.normalizeServerUrl('http://'),
        throwsA(isA<FormatException>()),
      );
    });
  });
}
