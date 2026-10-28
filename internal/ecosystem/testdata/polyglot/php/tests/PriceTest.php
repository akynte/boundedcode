<?php

require __DIR__ . '/../src/Price.php';

$p = new Shop\Price(200);
if ($p->discounted(10) !== 180) {
    fwrite(STDERR, "discounted\n");
    exit(1);
}
echo "ok\n";
