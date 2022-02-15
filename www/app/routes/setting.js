import Ember from 'ember';

export default Ember.Route.extend({

  auth: Ember.inject.service('auth'),
  
  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },

  // model() {
  //   return this.get('api').getMeetups();
  // }

});
