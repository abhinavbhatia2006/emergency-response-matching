import {defineConfig} from 'vite';
export default defineConfig({server:{proxy:{'/api/resources':{target:'http://localhost:8082',rewrite:p=>p.replace(/^\/api/,'')},'/api':{target:'http://localhost:8081',rewrite:p=>p.replace(/^\/api/,'')}}}});
