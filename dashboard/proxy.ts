import { NextResponse, type NextRequest } from 'next/server';

// Optional HTTP Basic auth for the whole dashboard (DASHBOARD_USER + DASHBOARD_PASSWORD).
export function proxy(req: NextRequest) {
  const user = process.env.DASHBOARD_USER;
  const pass = process.env.DASHBOARD_PASSWORD;
  if (!user || !pass) return NextResponse.next();
  const header = req.headers.get('authorization') || '';
  if (header.startsWith('Basic ')) {
    const [u, p] = atob(header.slice(6)).split(':');
    if (u === user && p === pass) return NextResponse.next();
  }
  return new NextResponse('Authentication required', { status: 401, headers: { 'WWW-Authenticate': 'Basic realm="Calor"' } });
}

export const config = {
  matcher: ['/((?!_next/static|_next/image|favicon.ico).*)'],
};
