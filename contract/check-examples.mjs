// Checks every file in examples/ against the component schema it is named for, so the examples and the
// spec cannot drift apart. The mapping is explicit: a new example needs a line here.
import { readFileSync } from 'node:fs';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { parse } from 'yaml';

const spec = parse(readFileSync(new URL('./openapi.yaml', import.meta.url), 'utf8'));
const examples = {
  'live.json': 'Live',
  'summary.json': 'Summary',
  'ingest-pc.json': 'PcIngest',
  'rates.json': 'RateConfig',
  'computer-detail.json': 'ComputerDetail',
  'group-detail.json': 'GroupDetail',
  'computer-series.json': 'PcSeries',
  'computer-history.json': 'ComputerHistory',
};

const ajv = new Ajv2020({ strict: false, allErrors: true });
addFormats(ajv);
ajv.addSchema({ $id: 'spec', components: spec.components });

let failed = false;
for (const [file, schemaName] of Object.entries(examples)) {
  const data = JSON.parse(readFileSync(new URL(`./examples/${file}`, import.meta.url), 'utf8'));
  const validate = ajv.compile({ $ref: `spec#/components/schemas/${schemaName}` });
  if (validate(data)) {
    console.log(`ok   ${file} matches ${schemaName}`);
  } else {
    failed = true;
    console.error(`FAIL ${file} does not match ${schemaName}`);
    for (const e of validate.errors) console.error(`  ${e.instancePath || '/'} ${e.message}`);
  }
}
process.exit(failed ? 1 : 0);
