import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('a tap does not run pan completion logic', (tester) async {
    var panStarted = false;
    var panCancelCount = 0;
    var panCompletionCount = 0;
    var tapCount = 0;

    await tester.pumpWidget(
      Directionality(
        textDirection: TextDirection.ltr,
        child: Center(
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: () => tapCount++,
            onPanStart: (_) => panStarted = true,
            onPanEnd: (_) {
              if (panStarted) {
                panStarted = false;
                panCompletionCount++;
              }
            },
            onPanCancel: () {
              panCancelCount++;
              if (panStarted) {
                panStarted = false;
                panCompletionCount++;
              }
            },
            child: const SizedBox(width: 240, height: 160),
          ),
        ),
      ),
    );

    await tester.tap(find.byType(GestureDetector));
    await tester.pump();

    expect(tapCount, 1);
    expect(panCancelCount, 1);
    expect(panCompletionCount, 0);
  });
}
