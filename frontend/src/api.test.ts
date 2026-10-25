import {afterEach,expect,it,vi} from 'vitest';
import {api} from './api';
afterEach(()=>vi.unstubAllGlobals());
it('preserves actionable API errors',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({message:'Resource belongs to an assignment'}),{status:409})));
 await expect(api('/resources/id/status','PUT',{status:'AVAILABLE'})).rejects.toThrow('Resource belongs to an assignment');
});
it('handles unavailable HTML upstream responses',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response('<h1>Bad gateway</h1>',{status:502})));
 await expect(api('/requests')).rejects.toThrow('Service unavailable');
});
