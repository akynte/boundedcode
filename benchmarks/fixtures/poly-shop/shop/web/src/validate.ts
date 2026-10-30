import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The schema every service shares (schema/order.schema.json).
const schema = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../schema/order.schema.json'), 'utf8'));

// orderErrors lists the ways an order read from JSON breaks the shared
// schema: missing required properties and wrong types; empty when valid.
export function orderErrors(o: Record<string, unknown>): string[] {
  const out: string[] = [];
  for (const name of schema.required ?? []) {
    if (!(name in o)) out.push(`missing ${name}`);
  }
  const types: Record<string, (v: unknown) => boolean> = {
    string: (v) => typeof v === 'string',
    integer: (v) => Number.isInteger(v),
    array: (v) => Array.isArray(v),
  };
  for (const [name, prop] of Object.entries(schema.properties ?? {}) as [string, { type: string }][]) {
    if (name in o && types[prop.type] && !types[prop.type](o[name])) out.push(`${name} is not ${prop.type}`);
  }
  return out;
}
