/* Regenerate netchan_expected.txt:
 *   gcc -m64 -O1 -std=gnu89 -fcommon -fno-strict-aliasing -I oracle/build/src/qcommon \
 *       -o drv netchan_driver.c oracle/build/src/qcommon/net_chan.c
 *   ./drv < netchan_script.txt > netchan_expected.txt
 * MSG/SZ helpers below are verbatim copies of qcommon/common.c. */
#include "qcommon.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int curtime;
static byte lastsent[2][MAX_MSGLEN]; static int lastlen[2];
static int cursender;
cvar_t cv_show = {"x","0"}; cvar_t cv_qport;
cvar_t *Cvar_Get (char *var_name, char *value, int flags) {
	if (!strcmp(var_name,"qport")) return &cv_qport;
	return &cv_show;
}
int Sys_Milliseconds(void){return 0;}
char *va(char *format, ...){static char s[1024]; va_list a; va_start(a,format); vsprintf(s,format,a); va_end(a); return s;}
void Com_Printf (char *fmt, ...){ va_list a; char s[1024]; va_start(a,fmt); vsprintf(s,fmt,a); va_end(a); printf("print %s", s); }
void Com_Error (int code, char *fmt, ...){ printf("error\n"); exit(1);}
char *NET_AdrToString (netadr_t a){ return "adr"; }
void NET_SendPacket (netsrc_t sock, int length, void *data, netadr_t to) {
	int i; memcpy(lastsent[cursender], data, length); lastlen[cursender]=length;
	printf("sent "); for(i=0;i<length;i++) printf("%02x", ((byte*)data)[i]); printf("\n");
}
/* verbatim from qcommon/common.c */
void SZ_Init (sizebuf_t *buf, byte *data, int length) { memset (buf, 0, sizeof(*buf)); buf->data = data; buf->maxsize = length; }
void SZ_Clear (sizebuf_t *buf) { buf->cursize = 0; buf->overflowed = false; }
void *SZ_GetSpace (sizebuf_t *buf, int length) {
	void *data;
	if (buf->cursize + length > buf->maxsize) {
		if (!buf->allowoverflow) Com_Error (ERR_FATAL, "SZ_GetSpace: overflow without allowoverflow set");
		if (length > buf->maxsize) Com_Error (ERR_FATAL, "SZ_GetSpace: %i is > full buffer size", length);
		Com_Printf ("SZ_GetSpace: overflow\n");
		SZ_Clear (buf); buf->overflowed = true;
	}
	data = buf->data + buf->cursize; buf->cursize += length; return data;
}
void SZ_Write (sizebuf_t *buf, void *data, int length) { memcpy (SZ_GetSpace(buf,length),data,length); }
void MSG_WriteShort (sizebuf_t *sb, int c) { byte *buf = SZ_GetSpace (sb, 2); buf[0] = c&0xff; buf[1] = c>>8; }
void MSG_WriteLong (sizebuf_t *sb, int c) { byte *buf = SZ_GetSpace (sb, 4); buf[0] = c&0xff; buf[1] = (c>>8)&0xff; buf[2] = (c>>16)&0xff; buf[3] = c>>24; }
void MSG_BeginReading (sizebuf_t *msg) { msg->readcount = 0; }
int MSG_ReadShort (sizebuf_t *msg_read) { int c;
	if (msg_read->readcount+2 > msg_read->cursize) c = -1;
	else c = (short)(msg_read->data[msg_read->readcount] + (msg_read->data[msg_read->readcount+1]<<8));
	msg_read->readcount += 2; return c; }
int MSG_ReadLong (sizebuf_t *msg_read) { int c;
	if (msg_read->readcount+4 > msg_read->cursize) c = -1;
	else c = msg_read->data[msg_read->readcount] + (msg_read->data[msg_read->readcount+1]<<8) + (msg_read->data[msg_read->readcount+2]<<16) + (msg_read->data[msg_read->readcount+3]<<24);
	msg_read->readcount += 4; return c; }

static netchan_t ch[2];
static int idx(char *n){ return n[0]=='s'; }
static int unhex(char *h, byte *out){ int n=0; if(!strcmp(h,"-")) return 0; while(h[0]&&h[1]){ unsigned v; sscanf(h,"%2x",&v); out[n++]=v; h+=2;} return n; }
static void state(int i){ netchan_t *c=&ch[i];
	printf("state %c in=%d inack=%d inrack=%d inrel=%d out=%d rel=%d lastrel=%d rellen=%d dropped=%d fatal=%d msg=%d ovf=%d lr=%d ls=%d\n",
	 i?'s':'c', c->incoming_sequence,c->incoming_acknowledged,c->incoming_reliable_acknowledged,c->incoming_reliable_sequence,
	 c->outgoing_sequence,c->reliable_sequence,c->last_reliable_sequence,c->reliable_length,c->dropped,c->fatal_error,c->message.cursize,c->message.overflowed,c->last_received,c->last_sent); }
int main(void){
	char line[8192], op[32], a[32], b[4096]; byte buf[MAX_MSGLEN*2]; netadr_t adr; memset(&adr,0,sizeof adr);
	cv_qport.name="qport"; Netchan_Init();
	while(fgets(line,sizeof line,stdin)){
		int n; line[strcspn(line,"\n")]=0;
		if(!line[0]||line[0]=='#') continue;
		b[0]=0; a[0]=0;
		n=sscanf(line,"%31s %31s %4095s",op,a,b);
		printf("> %s\n",line);
		if(!strcmp(op,"time")) curtime=atoi(a);
		else if(!strcmp(op,"setup")){ int i=idx(a); int q=atoi(b); cv_qport.value=q; Netchan_Setup(i?NS_SERVER:NS_CLIENT,&ch[i],adr,q);}
		else if(!strcmp(op,"rel")){ int i=idx(a); int l=unhex(b,buf); SZ_Write(&ch[i].message,buf,l);}
		else if(!strcmp(op,"relfill")){ int i=idx(a); int l=atoi(b); memset(buf,0xab,l); SZ_Write(&ch[i].message,buf,l);}
		else if(!strcmp(op,"tx")){ int i=idx(a); int l=unhex(b,buf); cursender=i; Netchan_Transmit(&ch[i],l,buf);}
		else if(!strcmp(op,"txfill")){ int i=idx(a); int l=atoi(b); memset(buf,0xcd,l); cursender=i; Netchan_Transmit(&ch[i],l,buf);}
		else if(!strcmp(op,"rx")||!strcmp(op,"rxlast")){ int i=idx(a); sizebuf_t m; int l; byte mb[MAX_MSGLEN];
			if(op[2]) { int j=idx(b); l=lastlen[j]; memcpy(mb,lastsent[j],l);} else l=unhex(b,mb);
			SZ_Init(&m,mb,sizeof mb); m.cursize=l;
			{ qboolean ok=Netchan_Process(&ch[i],&m); printf("rx %d %d\n",ok,m.readcount);} }
		else { printf("badop\n"); }
		if(!strcmp(op,"time")) continue;
		state(0); state(1);
	}
	return 0;
}
