<?php

declare(strict_types=1);

// A minimal test runner: every function named test_* in tests/*Test.php
// (or the files given as arguments) runs; one that returns 'skip' is
// skipped, one that throws fails.

require_once __DIR__ . '/../src/Slug.php';

function assert_same($want, $got, string $what = ''): void
{
    if ($want !== $got) {
        throw new Exception(($what ? "$what: " : '') . 'want ' . var_export($want, true) . ', got ' . var_export($got, true));
    }
}

$files = array_slice($argv, 1) ?: glob(__DIR__ . '/*Test.php');
foreach ($files as $f) {
    require_once $f;
}
$ran = 0;
$failed = 0;
foreach (get_defined_functions()['user'] as $fn) {
    if (strpos($fn, 'test_') !== 0) {
        continue;
    }
    $ran++;
    try {
        if ($fn() === 'skip') {
            echo "skip $fn\n";
            continue;
        }
        echo "ok   $fn\n";
    } catch (Throwable $e) {
        $failed++;
        echo "FAIL $fn: " . $e->getMessage() . "\n";
    }
}
echo "$ran tests, $failed failed\n";
exit($failed ? 1 : 0);
