export interface User {
  id: string;
  roles: Role[];
}

export type Role = 'admin' | 'viewer';

export enum Level {
  Low,
  High,
}

export function isAdmin(u: User): boolean {
  return u.roles.includes('admin');
}

export class UserStore {
  private users: User[] = [];

  find(id: string): User | undefined {
    return this.users.find((u) => u.id === id);
  }
}
