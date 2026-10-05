package agentcode

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestClientCancelsAndExpiresQueuedWorkWithoutSendingIt(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeResponseServer(t, artifact.ContractHash, `
responses.push(frame.params.input.name);
setTimeout(()=>process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true,payload:[...responses]}})+"\n"),80);`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client=createClient({concurrency:1,executablePath,vaultPath});
try {
 await client.files({name:"warmup"});
 const controller=new AbortController();
 const first=client.files({name:"first"});
 const cancelled=client.files({name:"cancelled"},{signal:controller.signal});
 const expired=client.files({name:"expired"},{timeoutMs:5});
 const settled=Promise.allSettled([first,cancelled,expired]);
 controller.abort();
 const results=await settled;
 const final=await client.files({name:"final"});
 console.log(JSON.stringify({calls:final.payload,errors:results.slice(1).map(r=>({code:r.reason.code,mayHaveExecuted:r.reason.details.mayHaveExecuted}))}));
} finally {await client.close();}`)
	require.Equal(t, []any{"warmup", "first", "final"}, result["calls"])
	require.Equal(t, []any{map[string]any{"code": "cancelled", "mayHaveExecuted": false}, map[string]any{"code": "deadline_exceeded", "mayHaveExecuted": false}}, result["errors"])
}

func TestUnacknowledgedCancellationDoesNotInterruptQueuedWork(t *testing.T) {
	requireNode(t)
	artifact, err := Generate(t.TempDir(), []string{"files"})
	require.NoError(t, err)
	fake := writeResponseServer(t, artifact.ContractHash, `
const name=frame.params.input.name;
if(name === "second" && responses.firstPending) process.exit(55);
responses.push(name);
if(name === "first") responses.firstPending=true;
setTimeout(()=>{if(name === "first") responses.firstPending=false; process.stdout.write(JSON.stringify({jsonrpc:"2.0",id:frame.id,result:{ok:true,payload:[...responses]}})+"\n");},name === "first" ? 1400 : 0);`)
	result := runNode(t, artifact.ModulePath, fake, t.TempDir(), `
const client=createClient({concurrency:1,executablePath,vaultPath});
try {
 await client.files({name:"warmup"});
 const controller=new AbortController();
 const first=client.files({name:"first"},{signal:controller.signal});
 const second=client.files({name:"second"});
 const settled=Promise.allSettled([first,second]);
 setTimeout(()=>controller.abort(),20);
 const [cancelled,completed]=await settled;
 console.log(JSON.stringify({code:cancelled.reason.code,mayHaveExecuted:cancelled.reason.details.mayHaveExecuted,status:completed.status,calls:completed.value?.payload}));
} finally {await client.close();}`)
	require.Equal(t, "cancelled", result["code"])
	require.Equal(t, true, result["mayHaveExecuted"])
	require.Equal(t, "fulfilled", result["status"])
	require.Equal(t, []any{"warmup", "first", "second"}, result["calls"])
}
