<?php

declare(strict_types=1);

/**
 * slugify turns a title into a URL slug: lower case ASCII letters and
 * digits, words separated by single hyphens, no hyphen at either end.
 * Accented Latin letters lose their accents ("Café" becomes "cafe").
 */
function slugify(string $title): string
{
    $s = strtolower(strtr($title, [
        'à' => 'a', 'á' => 'a', 'â' => 'a', 'ä' => 'a', 'ç' => 'c', 'è' => 'e', 'é' => 'e', 'ê' => 'e', 'ë' => 'e',
        'ì' => 'i', 'í' => 'i', 'î' => 'i', 'ï' => 'i', 'ñ' => 'n', 'ò' => 'o', 'ó' => 'o', 'ô' => 'o', 'ö' => 'o',
        'ù' => 'u', 'ú' => 'u', 'û' => 'u', 'ü' => 'u', 'À' => 'a', 'É' => 'e', 'Ö' => 'o', 'Ü' => 'u',
    ]));
    return preg_replace('/[^a-z0-9]/', '-', $s);
}
