export namespace main {
	
	export class AppSettings {
	    autoHash: boolean;
	    compress: boolean;
	    cacheManifest: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoHash = source["autoHash"];
	        this.compress = source["compress"];
	        this.cacheManifest = source["cacheManifest"];
	    }
	}
	export class TransferRecord {
	    // Go type: time
	    timestamp: any;
	    path: string;
	    fullPath: string;
	    size: number;
	    direction: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new TransferRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.path = source["path"];
	        this.fullPath = source["fullPath"];
	        this.size = source["size"];
	        this.direction = source["direction"];
	        this.status = source["status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

