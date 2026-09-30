export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <main className="mx-auto grid min-h-dvh w-full max-w-sm content-center gap-6 px-4 py-10">
      <p className="text-center text-xl font-semibold tracking-tight">Meal Planner</p>
      {children}
    </main>
  );
}
