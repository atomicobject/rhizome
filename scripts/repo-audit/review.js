// A reviewer supplies a JSON array following the README decision contract.
const fs = await import('node:fs/promises');
const { recordReviews } = await import(process.cwd() + '/scripts/repo-audit/reviews.mjs');
if (!process.env.AUDIT_OUTPUT || !process.env.AUDIT_VERDICTS)
  throw new Error('Set AUDIT_OUTPUT and AUDIT_VERDICTS to the audit directory and decision JSON file');
return await recordReviews(rzm, process.env.AUDIT_OUTPUT,
  JSON.parse(await fs.readFile(process.env.AUDIT_VERDICTS, 'utf8')));
