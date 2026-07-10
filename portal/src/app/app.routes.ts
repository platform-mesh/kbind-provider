import { Routes } from '@angular/router';
import { ConnectClusterComponent } from './connect-cluster/connect-cluster.component';

export const routes: Routes = [
  { path: '', redirectTo: 'connect-cluster', pathMatch: 'full' },
  { path: 'connect-cluster', component: ConnectClusterComponent },
  { path: '**', redirectTo: 'connect-cluster' },
];
