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

console.log("Debug enabled - "+config.debug.output);

io.sockets.on('connection', function (socket) {
  var redisClient = null;
  
  try {
	  if (config.use_publish_notifications == true && config.ignore_desktop_client == false) {
		  redisClient = redis.createClient(config.redis.port,config.redis.host,config.redis.options);
			  
		  redisClient.on("message", function(channel, message) {		   
			  	if (channel.indexOf('admin_room_') !== -1) {	  
			  		socket.emit('syncbackoffice',message);
		  		} else {
		  			socket.emit('syncforce', message);
		  		}
		 });
	  }
  } catch (e) {
		if (config.debug.output == true) {
			throw e;
		}
  }
  
  socket.on('newmessage', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('newmessage:'+data.instance_id+'_'+data.chat_id); 	
		  };

		  if (data.data.message_id != "0") {
			  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('newmessage', data);
		  }
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });
  
  socket.on('userpostedmessage', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('userpostedmessage:'+data.instance_id+'_'+data.chat_id); 	
		  };  		
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userpostedmessage', data);    	
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });
  
  socket.on('userstartedpostmessage', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('userstartedpostmessage:'+data.instance_id+'_'+data.chat_id); 	
		  };  		
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userstartedpostmessage', data);    
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });

  socket.on('usertyping', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('usertyping:'+data.instance_id+'_'+data.chat_data.chat_id+'-'+data.chat_data.status); 
		  };	
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_data.chat_id).emit('usertyping', data.chat_data);
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });

  socket.on('operatortyping', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('operatortyping:'+data.instance_id+'_'+data.chat_data.chat_id+'-'+data.chat_data.status);
		  }; 	
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_data.chat_id).emit('operatortyping', data.chat_data);
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });

  socket.on('userleftchat', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('userleftchat:'+data.instance_id+'_'+data.chat_id);
		  }; 	
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userleftchat', data.chat_id);
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });
  
  socket.on('join', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('join:'+data.instance_id+'_'+data.chat_id);  
		  };		
		  socket.join('chat_room_'+data.instance_id+'_'+data.chat_id);
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('userjoined', data.chat_id);

		  if (config.use_publish_notifications == true && config.ignore_desktop_client == false) {
			  if (config.debug.output == true) {
				  console.log('subscribed_to_channel:'+'chat_room_' + data.instance_id + '_' + data.chat_id);
			  }
			  redisClient.subscribe('chat_room_' + data.instance_id + '_' + data.chat_id); 
		  }
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }
  });

  socket.on('join_admin', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('join_admin:'+data.instance_id);  
		  };

		  if (redis !== undefined) {	  		
			  if (config.use_publish_notifications == true && config.ignore_desktop_client == true) {
				  redisClient = redis.createClient(config.redis.port,config.redis.host,config.redis.options);  				
				  redisClient.on("message", function(channel, message) {          
					  socket.emit('syncbackoffice',message);
				  });  				
			  };
			  redisClient.subscribe('admin_room_' + data.instance_id);
		  }
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }	    
  });

  socket.on('leave', function (data) {
	  try {
		  if (config.debug.output == true) {
			  console.log('leave:'+data.instance_id+'_'+data.chat_id);
		  };
		  socket.leave('chat_room_'+data.instance_id+'_'+data.chat_id);

		  if (config.use_publish_notifications == true && config.ignore_desktop_client == false) {
			  redisClient.unsubscribe('chat_room_' + data.instance_id + '_' + data.chat_id); 
		  }
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }	
  });

  socket.on('syncforce', function (data) { 
	  try {
		  if (config.debug.output == true) {
			  console.log('syncforce:'+data.instance_id+'_'+data.chat_id); 	
		  };
		  socket.broadcast.to('chat_room_'+data.instance_id+'_'+data.chat_id).emit('syncforce', data.chat_id);
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }	
  });
  
  socket.on('disconnect', function() {
	  try {
		  if (redisClient !== null) {
			  redisClient.quit();
		  }
	  } catch (e) {
		  if (config.debug.output == true) {
			  throw e;
		  }
	  }	
  }); 
  
});