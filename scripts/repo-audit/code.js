// Run from the vault root: rzm agent code execute --timeout 8h < scripts/repo-audit/code.js
const { runAudit } = await import(process.cwd() + '/scripts/repo-audit/run.mjs');
return await runAudit(rzm, 'code');
