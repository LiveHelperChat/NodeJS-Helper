var http = require('http').createServer(handler)
   , config = require('./settings'); 

if (config.use_publish_notifications == true) {
	const redis = require('redis');
	const redisClient = redis.createClient(config.redis.port,config.redis.host,config.redis.options);
}

var io = require(config.socketiopath).listen(http);

// enable all transports (optional if you want flashsocket support, please note that some hosting
// providers do not allow you to create servers that listen on a port different than 80 or their
// default port)
io.set('transports',[
    'websocket',
    'polling',
    'xhr-polling',
    'jsonp-polling',
    'polling']);

http.listen(config.web.port,config.web.host);
console.log('LHC Server listening on '+config.web.host+':'+config.web.port);

function handler(req, res) {
  res.writeHead(200);
  res.end();
}

io.sockets.on('connection', function (socket) {
  var redisClient = null;
  
  socket.on('newmessage', function (data) {
  		if (config.debug.output == true) {
  			console.log('newmessage:'+data.instance_id+'_'+data.chat_id); 	
  		};
  		if (data.data.message_id != "0"){
  			socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('newmessage', data);
    	};
  });
  
  socket.on('userpostedmessage', function (data) {
	  if (config.debug.output == true) {
		  console.log('userpostedmessage:'+data.instance_id+'_'+data.chat_id); 	
	  };  		
	  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userpostedmessage', data);    	
  });
  
  socket.on('userstartedpostmessage', function (data) {
		if (config.debug.output == true) {
			console.log('userstartedpostmessage:'+data.instance_id+'_'+data.chat_id); 	
		};  		
		socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userstartedpostmessage', data);    	
  });
  
  socket.on('usertyping', function (data) {
  		if (config.debug.output == true) {
  			console.log('usertyping:'+data.instance_id+'_'+data.chat_data.chat_id+'-'+data.chat_data.status); 
  		};	
    	socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_data.chat_id).emit('usertyping', data.chat_data)
  });

  socket.on('operatortyping', function (data) {
  		if (config.debug.output == true) {
  			console.log('operatortyping:'+data.instance_id+'_'+data.chat_data.chat_id+'-'+data.chat_data.status);
  		}; 	
    	socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_data.chat_id).emit('operatortyping', data.chat_data)
  });

  socket.on('userleftchat', function (data) {
  		if (config.debug.output == true) {
  			console.log('userleftchat:'+data.instance_id+'_'+data.chat_id);
  		}; 	
    	socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userleftchat', data.chat_id);
  });
  
  socket.on('join', function (data) {
	  if (config.debug.output == true) {
		  console.log('join:'+data.instance_id+'_'+data.chat_id);  
	  };		
	  socket.join('chat_room_'+data.instance_id+'_'+data.chat_id);
	  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userjoined', data.chat_id);
  });

  socket.on('join_admin', function (data) {
  		if (config.debug.output == true) {
  			console.log('join_admin:'+data.instance_id);  
  		};
  		if (redis !== undefined) {
	  		redisClient = redis.createClient(config.redis.port,config.redis.host,config.redis.options);
	  	    redisClient.subscribe('admin_room_' + data.instance_id); 
	  	    
	  	    redisClient.on("message", function(channel, message) {          
	  		  socket.emit('syncbackoffice',message);
	        });  
  	    };	    
  });

  socket.on('leave', function (data) {
  		if (config.debug.output == true) {
  			console.log('leave:'+data.instance_id+'_'+data.chat_id);
  		};
  		socket.leave('chat_room_'+data.instance_id+'_'+data.chat_id);
  });
  
  socket.on('syncforce', function (data) { 
  		if (config.debug.output == true) {
  			console.log('syncforce:'+data.instance_id+'_'+data.chat_id); 	
  		};
    	socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('syncforce', data.chat_id)
  });
  
  socket.on('disconnect', function() {
	  if (redisClient !== null) {
		  redisClient.quit();
      }
  }); 
  
});