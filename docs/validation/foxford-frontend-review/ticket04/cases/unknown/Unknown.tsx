import React from 'react';
import { vm } from './project-contract';
export function Unknown() { return <span>{vm.state.count.use()}</span>; }
