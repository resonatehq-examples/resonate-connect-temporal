import { Resonate, Context } from "@resonatehq/sdk";

const resonate = Resonate.remote({
  url: process.env.RESONATE_URL || "http://localhost:8001",
  group: process.env.RESONATE_GROUP || "default",
});

/**
 * Greet function - called as an activity from Temporal
 */
function* greet(ctx: Context, name: string) {
  console.log(`[Resonate] greet called with: ${name}`);
  return `Hello, ${name}!`;
}

/**
 * Transform function - transforms data
 */
function* transform(ctx: Context, data: string[]) {
  console.log(`[Resonate] transform called with: ${data}`);
  return data.map((item) => item.toUpperCase());
}

/**
 * Aggregate function - aggregates results
 */
function* aggregate(ctx: Context, data: string[]) {
  console.log(`[Resonate] aggregate called with: ${data}`);
  return {
    count: data.length,
    items: data,
    summary: data.join(", "),
  };
}

// Register functions
resonate.register(greet);
resonate.register(transform);
resonate.register(aggregate);

// Log startup info
console.log("==================================================");
console.log("  Resonate Worker");
console.log("==================================================");
console.log(`  URL: ${process.env.RESONATE_URL || "http://localhost:8001"}`);
console.log(`  Group: ${process.env.RESONATE_GROUP || "default"}`);
console.log("  Functions: greet, transform, aggregate");
console.log("==================================================");
console.log();
console.log("Worker started, waiting for invocations...");

// Keep the process alive
setInterval(() => {}, 1 << 30);
