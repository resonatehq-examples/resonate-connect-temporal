import { Resonate, type Context } from "@resonatehq/sdk";

const resonate = Resonate{
  url: process.env.RESONATE_URL || "http://localhost:8001",
});

/**
 * A Resonate function that calls a Temporal workflow.
 */
function* myResonateFunction(ctx: Context, name: string) {
  const response = yield* ctx.rpc<{ result: string }>(
    "GreetingWorkflow",
    name,
    ctx.options({ target: "poll://any@temporal" })
  );

  return response.result;
}

resonate.register(myResonateFunction);

async function main() {
  const id = `greeting-${Date.now()}`;
  console.log(`Invoking myResonateFunction with id: ${id}`);

  const result = await resonate.run(id, myResonateFunction, "Alice");
  console.log(`Result: ${result}`);
}

main().catch(console.error);
