export default function AdminLoading() {
  return (
    <div className="space-y-4">
      <div className="skeleton h-8 w-48" />
      <div className="skeleton h-4 w-64" />
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="card p-5 space-y-3">
          <div className="skeleton h-6 w-3/4" />
          <div className="skeleton h-4 w-1/2" />
          <div className="flex gap-2">
            <div className="skeleton h-8 w-24" />
            <div className="skeleton h-8 w-24" />
          </div>
        </div>
      ))}
    </div>
  );
}
